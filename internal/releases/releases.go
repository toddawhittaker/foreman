// Package releases asks Docker Hub which factoriotools/factorio images exist
// and factorio.com which releases are current (SPEC 10.2, 10.7, 14.10).
// Nothing here runs at startup or on a timer: callers ask when a page needs it.
package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	realTagsURL   = "https://hub.docker.com/v2/repositories/factoriotools/factorio/tags"
	realLatestURL = "https://factorio.com/api/latest-releases"

	callTimeout   = 10 * time.Second
	cacheLifetime = 5 * time.Minute
	maxTagPage    = 2 << 20
	maxLatest     = 64 << 10
	pageSize      = 100
	maxPages      = 10
)

// lines are the game lines Foreman supports (SPEC 10.8). Each tag is also
// checked with validVersion, so a stale entry here cannot let an unsupported
// version through.
var lines = []string{"2.1", "2.0", "1.1"}

// ErrUnreachable wraps every network, status, size and parse failure.
var ErrUnreachable = errors.New("release information is unreachable")

// Latest holds the headless versions of the current stable and experimental
// releases.
type Latest struct{ Stable, Experimental string }

type entry[T any] struct {
	mu      sync.Mutex // held during a fetch, so only one runs at a time
	value   T
	expires time.Time
	ok      bool
}

// Client fetches and caches release information.
type Client struct {
	hubURL, latestURL string
	hc                *http.Client
	now               func() time.Time

	versions entry[[]string]
	latest   entry[Latest]
}

// New returns a client for the real Docker Hub and factorio.com.
func New() *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyFromEnvironment
	hc := &http.Client{
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are not followed")
		},
	}
	return newClient(realTagsURL, realLatestURL, hc, time.Now)
}

func newClient(hubURL, latestURL string, hc *http.Client, now func() time.Time) *Client {
	return &Client{hubURL: hubURL, latestURL: latestURL, hc: hc, now: now}
}

// Versions lists the published patch releases of the supported lines, newest
// first, without duplicates.
func (c *Client) Versions(ctx context.Context) ([]string, error) {
	return cached(ctx, c, &c.versions, c.fetchVersions, func(v []string) []string {
		return append([]string(nil), v...)
	})
}

// Latest returns the current stable and experimental releases.
func (c *Client) Latest(ctx context.Context) (Latest, error) {
	return cached(ctx, c, &c.latest, c.fetchLatest, func(l Latest) Latest { return l })
}

// cached serves e while it is fresh and otherwise fetches under e's lock.
// Errors are not cached.
func cached[T any](ctx context.Context, c *Client, e *entry[T], fetch func(context.Context) (T, error), clone func(T) T) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ok && c.now().Before(e.expires) {
		return clone(e.value), nil
	}
	v, err := fetch(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	e.value, e.expires, e.ok = v, c.now().Add(cacheLifetime), true
	return clone(v), nil
}

func (c *Client) fetchVersions(ctx context.Context) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, line := range lines {
		for page := 1; page <= maxPages; page++ {
			names, more, err := c.tagPage(ctx, line, page)
			if err != nil {
				return nil, err
			}
			for _, n := range names {
				if !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
			if !more {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i], out[j]) > 0 })
	return out, nil
}

// tagPage returns the usable version names on one page and whether Docker
// Hub says another page follows. The page's own "next" link is never fetched.
func (c *Client) tagPage(ctx context.Context, line string, page int) ([]string, bool, error) {
	u, err := url.Parse(c.hubURL)
	if err != nil {
		return nil, false, fmt.Errorf("%w: bad tag list address", ErrUnreachable)
	}
	q := u.Query()
	q.Set("name", line+".")
	q.Set("page_size", strconv.Itoa(pageSize))
	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()

	body, err := c.get(ctx, u.String(), maxTagPage)
	if err != nil {
		return nil, false, err
	}
	var doc struct {
		Next    *string `json:"next"`
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, false, fmt.Errorf("%w: the tag list is not valid JSON", ErrUnreachable)
	}
	var names []string
	for _, r := range doc.Results {
		if strings.HasPrefix(r.Name, line+".") && validVersion(r.Name) {
			names = append(names, r.Name)
		}
	}
	return names, doc.Next != nil && *doc.Next != "", nil
}

func (c *Client) fetchLatest(ctx context.Context) (Latest, error) {
	body, err := c.get(ctx, c.latestURL, maxLatest)
	if err != nil {
		return Latest{}, err
	}
	var doc struct {
		Stable struct {
			Headless string `json:"headless"`
		} `json:"stable"`
		Experimental struct {
			Headless string `json:"headless"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return Latest{}, fmt.Errorf("%w: the release list is not valid JSON", ErrUnreachable)
	}
	// A release on a game line Foreman does not support yet leaves its own
	// field empty; it must not hide the other one.
	var out Latest
	if validVersion(doc.Stable.Headless) {
		out.Stable = doc.Stable.Headless
	}
	if validVersion(doc.Experimental.Headless) {
		out.Experimental = doc.Experimental.Headless
	}
	if out == (Latest{}) {
		return Latest{}, fmt.Errorf("%w: the release list has no usable version", ErrUnreachable)
	}
	return out, nil
}

// get fetches rawURL and returns at most limit bytes of body; a longer body
// is an error. The body never appears in an error.
func (c *Client) get(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: bad address", ErrUnreachable)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrUnreachable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: the reply is too large", ErrUnreachable)
	}
	return body, nil
}

var validVersion = world.SupportedGameVersion

// compareVersions orders two valid versions by their three numbers.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range pa {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
