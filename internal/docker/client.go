// Package docker is Foreman's hand-written Docker Engine API client
// (STACK.md section 5). Every request is checked against SPEC 14.3.
package docker

import (
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
	http *http.Client
	base string // scheme and host, for example "http://docker"

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
		return &Client{http: newHTTPClient(tr), base: "http://docker"}, nil
	case "tcp":
		if u.Host == "" {
			return nil, fmt.Errorf("DOCKER_HOST %q names no host", dockerHost)
		}
		return &Client{http: newHTTPClient(nil), base: "http://" + u.Host}, nil
	default:
		return nil, fmt.Errorf("DOCKER_HOST %q must start with unix:// or tcp://", dockerHost)
	}
}

// newHTTPClient refuses redirects: a daemon or proxy must not be able to
// steer a request to a path the SPEC 14.3 check never saw.
func newHTTPClient(tr http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("the Docker daemon answered with a redirect, which Foreman does not follow")
		},
	}
}

// do sends one request after checking it against SPEC 14.3. Versioned
// requests carry the agreed API version. The caller closes the body.
func (c *Client) do(ctx context.Context, method, path string, versioned bool) (*http.Response, error) {
	if !allowed(method, path) {
		return nil, fmt.Errorf("refusing Docker API call %s %s: it is not on Foreman's allowed list (SPEC 14.3)", method, path)
	}
	full := c.base + path
	if versioned {
		v, err := c.APIVersion(ctx)
		if err != nil {
			return nil, err
		}
		full = c.base + "/v" + v + path
	}
	req, err := http.NewRequestWithContext(ctx, method, full, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the Docker daemon: %w", err)
	}
	if resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &APIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(msg))}
	}
	return resp, nil
}

// APIError is a non-success answer from the daemon.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("the Docker daemon answered %d: %s", e.Status, e.Message)
}

// VersionInfo is the part of GET /version Foreman uses.
type VersionInfo struct {
	Version    string `json:"Version"`
	APIVersion string `json:"ApiVersion"`
}

// Version returns the daemon's version.
func (c *Client) Version(ctx context.Context) (VersionInfo, error) {
	var v VersionInfo
	resp, err := c.do(ctx, http.MethodGet, "/version", false)
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

// Mount is one mount of a container.
type Mount struct {
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

// Container is the part of GET /containers/{id}/json Foreman uses.
type Container struct {
	Mounts []Mount
}

// InspectContainer reads one container's mounts.
func (c *Client) InspectContainer(ctx context.Context, id string) (Container, error) {
	if id == "" || strings.ContainsAny(id, "/?#%") || strings.Contains(id, "..") {
		return Container{}, fmt.Errorf("container id %q is not valid", id)
	}
	resp, err := c.do(ctx, http.MethodGet, "/containers/"+id+"/json", true)
	if err != nil {
		return Container{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		Mounts []Mount `json:"Mounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Container{}, fmt.Errorf("could not read container %s from the Docker daemon: %w", id, err)
	}
	return Container{Mounts: raw.Mounts}, nil
}
