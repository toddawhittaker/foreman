package modportal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	realBase = "https://mods.factorio.com"

	callTimeout     = 10 * time.Second
	downloadTimeout = 5 * time.Minute
	cacheLifetime   = 15 * time.Minute
	maxList         = 16 << 20
	maxFull         = 2 << 20
	maxDownload     = 200 << 20
	maxRedirects    = 2
	maxFullAtOnce   = 4
)

// Client asks the mod portal for mod lists, mod details and mod files
// (SPEC 9.1, 9.12, 14.10). Nothing here runs at startup or on a timer: a
// caller asks when a page needs it.
type Client struct {
	base string
	api  *http.Client // follows no redirects
	dl   *http.Client // follows the redirects the download rules allow
	now  func() time.Time

	callTimeout     time.Duration
	downloadTimeout time.Duration
	maxDownload     int64

	full  chan struct{} // holds a slot for each /full request in flight
	lists cache[[]listEntry]
	mods  cache[Mod]
	// missing remembers the names the portal answered 404 for: an answer,
	// kept as long as a found mod is.
	missing cache[struct{}]
}

// New returns a client for the real mod portal.
func New() *Client {
	return newClient(realBase, redirectAllowed)
}

// NewForTest returns a client for a fake portal at base. Downloads may be
// redirected to base's own host only, since the real rule asks for https.
func NewForTest(base string) *Client {
	u, err := url.Parse(base)
	if err != nil {
		panic(err)
	}
	return newClient(strings.TrimRight(base, "/"), func(r *url.URL) bool { return r.Host == u.Host })
}

func newClient(base string, redirectOK func(*url.URL) bool) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyFromEnvironment
	return &Client{
		base: base,
		api: &http.Client{
			Transport:     tr,
			CheckRedirect: func(*http.Request, []*http.Request) error { return errNoRedirects },
		},
		dl: &http.Client{
			Transport: tr,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Go copies the previous address into Referer, and a download's
				// address carries the token in its query (SPEC 14.10).
				req.Header.Del("Referer")
				switch {
				case req.URL.Path == "/login" || strings.HasPrefix(req.URL.Path, "/login/"):
					return errLoginRedirect
				case len(via) > maxRedirects:
					return errTooManyRedirects
				case !redirectOK(req.URL):
					return errRedirectRefused
				}
				return nil
			},
		},
		now:             time.Now,
		callTimeout:     callTimeout,
		downloadTimeout: downloadTimeout,
		maxDownload:     maxDownload,
		full:            make(chan struct{}, maxFullAtOnce),
	}
}

// redirectAllowed is the real rule: https, on a factorio.com subdomain.
func redirectAllowed(u *url.URL) bool {
	return u.Scheme == "https" && strings.HasSuffix(u.Hostname(), ".factorio.com")
}

// Reasons a request stops. None carries a URL, since the token rides in the
// query string of a download.
var (
	errNoRedirects      = errors.New("redirects are not followed")
	errTooManyRedirects = errors.New("too many redirects")
	errRedirectRefused  = errors.New("redirected to an address that is not allowed")
	errLoginRedirect    = errors.New("redirected to the login page")
)

// statusError is a reply other than 200 OK.
type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("%v: status %d", ErrUnreachable, e.code) }
func (e *statusError) Unwrap() error { return ErrUnreachable }

// requestError rebuilds an error from the HTTP client without the request
// URL: Go's *url.Error prints the whole URL, and a download URL carries the
// token. secrets are also removed from any text that is left.
func requestError(err error, secrets ...string) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	switch {
	case errors.Is(err, errLoginRedirect):
		return fmt.Errorf("%w: %w", ErrUnauthorized, errLoginRedirect)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: timed out", ErrUnreachable)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%w: %w", ErrUnreachable, context.Canceled)
	}
	msg := err.Error()
	for _, s := range secrets {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "[removed]")
			msg = strings.ReplaceAll(msg, url.QueryEscape(s), "[removed]")
		}
	}
	return fmt.Errorf("%w: %s", ErrUnreachable, msg)
}

// get fetches a JSON reply of at most limit bytes. The body never appears in
// an error.
func (c *Client) get(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: bad address", ErrUnreachable)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.api.Do(req)
	if err != nil {
		return nil, requestError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, requestError(err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: the reply is too large", ErrUnreachable)
	}
	return body, nil
}
