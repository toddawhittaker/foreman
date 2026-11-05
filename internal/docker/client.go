// Package docker is Foreman's hand-written Docker Engine API client
// (STACK.md section 5). Every request is checked against SPEC 14.3.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// MinAPIVersion is the oldest Docker Engine API this client accepts.
	MinAPIVersion = "1.44"
	// MaxAPIVersion is the newest Docker Engine API this client is written for.
	MaxAPIVersion = "1.52"
)

// Client talks to one Docker daemon. Create it with New.
type Client struct {
	http *http.Client // short calls: a 30-second total limit
	long *http.Client // log follow, pull and stop: no total limit, each caller sets a deadline
	base string       // scheme and host, for example "http://docker"
	// target is DOCKER_HOST as shown to people: unix:///path or
	// tcp://host:port, never a user name or password.
	target string

	mu         sync.Mutex
	apiVersion string // agreed with the daemon; empty until first needed
}

// New returns a client for a DOCKER_HOST value: unix:///path or
// tcp://host:port. It does not contact the daemon.
func New(dockerHost string) (*Client, error) {
	u, err := url.Parse(dockerHost)
	if err != nil {
		return nil, fmt.Errorf("DOCKER_HOST %q is not a valid address", dockerHost)
	}
	switch u.Scheme {
	case "unix":
		if u.Path == "" {
			return nil, fmt.Errorf("DOCKER_HOST %q names no socket path", dockerHost)
		}
		sock := u.Path
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}}
		return newClient(tr, "http://docker", "unix://"+sock), nil
	case "tcp":
		if u.Host == "" {
			return nil, fmt.Errorf("DOCKER_HOST %q names no host", dockerHost)
		}
		return newClient(nil, "http://"+u.Host, "tcp://"+u.Host), nil
	default:
		return nil, fmt.Errorf("DOCKER_HOST %q must start with unix:// or tcp://", dockerHost)
	}
}

func newClient(tr http.RoundTripper, base, target string) *Client {
	return &Client{
		http:   newHTTPClient(tr, shortCallLimit),
		long:   newHTTPClient(tr, 0),
		base:   base,
		target: target,
	}
}

// shortCallLimit bounds every call that is not a stream or a wait.
const shortCallLimit = 30 * time.Second

// newHTTPClient refuses redirects: a daemon or proxy must not be able to
// steer a request to a path the SPEC 14.3 check never saw. A limit of zero
// means no total limit.
func newHTTPClient(tr http.RoundTripper, limit time.Duration) *http.Client {
	return &http.Client{
		Transport: tr,
		Timeout:   limit,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("the Docker daemon answered with a redirect, which Foreman does not follow")
		},
	}
}

// request is one call. The path is checked against SPEC 14.3; the query and
// body are not part of that check.
type request struct {
	method string
	path   string     // without the /vX.Y prefix or a query string
	query  url.Values // may be nil
	body   any        // encoded as JSON when not nil
	long   bool       // use the client with no total limit
}

// do sends one request after checking it against SPEC 14.3. Every path but
// /version and /_ping carries the agreed API version. The caller closes the
// body of a successful response.
func (c *Client) do(ctx context.Context, r request) (*http.Response, error) {
	if !Allowed(r.method, r.path) {
		return nil, fmt.Errorf("refusing Docker API call %s %s: it is not on Foreman's allowed list (SPEC 14.3)", r.method, r.path)
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return nil, err
	}
	u.Path = r.path
	if r.path != "/version" && r.path != "/_ping" {
		v, err := c.APIVersion(ctx)
		if err != nil {
			return nil, err
		}
		u.Path = "/v" + v + r.path
	}
	u.RawQuery = r.query.Encode()
	var body io.Reader
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			return nil, fmt.Errorf("could not encode the request to the Docker daemon: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.http
	if r.long {
		hc = c.long
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &UnreachableError{Target: c.target, Err: err}
	}
	if resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &APIError{Status: resp.StatusCode, Message: errorMessage(raw)}
	}
	return resp, nil
}

// errorMessage reads the daemon's {"message": "..."} error body, or falls
// back to the raw text.
func errorMessage(raw []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &m) == nil && m.Message != "" {
		return m.Message
	}
	return strings.TrimSpace(string(raw))
}

// Errors a caller can test for with errors.Is.
var (
	// ErrNotFound is a 404: no such container, image or endpoint.
	ErrNotFound = errors.New("not found")
	// ErrNotModified is a 304: starting a started container, or stopping a
	// stopped one.
	ErrNotModified = errors.New("already in that state")
	// ErrConflict is a 409: for example the container name is taken, or the
	// container is not running.
	ErrConflict = errors.New("conflict")
)

// UnreachableError is a call that got no answer from the daemon. Its text
// is a plain sentence for the admin; Err is the raw transport error, with
// the request's URL in it, for the log only.
type UnreachableError struct {
	Target string // DOCKER_HOST, without any credentials
	Err    error
}

func (e *UnreachableError) Error() string {
	return "Foreman cannot reach Docker at " + e.Target + " (DOCKER_HOST). Check that Docker, or the socket proxy, is running and that DOCKER_HOST points at it"
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// APIError is a non-success answer from the daemon.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("the Docker daemon answered %d: %s", e.Status, e.Message)
}

// Is makes errors.Is(err, ErrNotFound) and its two siblings work.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrNotModified:
		return e.Status == http.StatusNotModified
	case ErrConflict:
		return e.Status == http.StatusConflict
	}
	return false
}

// Ping asks the daemon whether it is up.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/_ping"})
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// VersionInfo is the part of GET /version Foreman uses.
type VersionInfo struct {
	Version    string `json:"Version"`
	APIVersion string `json:"ApiVersion"`
}

// Version returns the daemon's version.
func (c *Client) Version(ctx context.Context) (VersionInfo, error) {
	var v VersionInfo
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/version"})
	if err != nil {
		return v, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return v, fmt.Errorf("could not read the Docker daemon's version: %w", err)
	}
	return v, nil
}

// APIVersion returns the API version agreed with the daemon, asking it
// once and remembering the answer.
func (c *Client) APIVersion(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.apiVersion != "" {
		return c.apiVersion, nil
	}
	v, err := c.Version(ctx)
	if err != nil {
		return "", err
	}
	agreed, err := negotiate(v.APIVersion)
	if err != nil {
		return "", err
	}
	c.apiVersion = agreed
	return agreed, nil
}

// negotiate picks the daemon's API version capped at MaxAPIVersion and
// refuses anything below MinAPIVersion.
func negotiate(daemon string) (string, error) {
	if _, _, ok := parseVersion(daemon); !ok {
		return "", fmt.Errorf("the Docker daemon reported API version %q, which Foreman cannot read", daemon)
	}
	if versionLess(daemon, MinAPIVersion) {
		return "", fmt.Errorf("the Docker daemon speaks API version %s, but Foreman needs %s or newer; upgrade Docker Engine", daemon, MinAPIVersion)
	}
	if versionLess(MaxAPIVersion, daemon) {
		return MaxAPIVersion, nil
	}
	return daemon, nil
}

func parseVersion(v string) (major, minor int, ok bool) {
	a, b, found := strings.Cut(v, ".")
	if !found {
		return 0, 0, false
	}
	x, err1 := strconv.Atoi(a)
	y, err2 := strconv.Atoi(b)
	return x, y, err1 == nil && err2 == nil && x >= 0 && y >= 0
}

// versionLess compares two valid "major.minor" versions.
func versionLess(a, b string) bool {
	am, an, _ := parseVersion(a)
	bm, bn, _ := parseVersion(b)
	if am != bm {
		return am < bm
	}
	return an < bn
}
