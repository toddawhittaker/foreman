// Package updatecheck asks GitHub, in the background, whether a newer
// Foreman release than the running one is out, so About can say so
// (SPEC 14.10, 17). The last good answer is kept in memory only.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// LatestURL is GitHub's latest Foreman release; the e2e fake answers it too.
	LatestURL = "https://api.github.com/repos/toddawhittaker/foreman/releases/latest"

	// ReleaseTagURL is the release page a notice links to, followed by the plain version.
	ReleaseTagURL = "https://github.com/toddawhittaker/foreman/releases/tag/v"

	callTimeout = 10 * time.Second
	maxBody     = 64 << 10
	firstDelay  = time.Minute
	checkEvery  = 6 * time.Hour
)

// Checker runs the check and holds its answer.
type Checker struct {
	url     string
	hc      *http.Client
	running string
	enabled func(context.Context) bool
	log     *slog.Logger
	first   time.Duration
	every   time.Duration
	limit   time.Duration // the most one call to GitHub may take
	soon    chan struct{} // CheckSoon's request to Run, at most one waiting

	mu     sync.Mutex
	latest string // the newer release's plain version, or empty
}

// New returns a checker for the real GitHub. running is this build's
// version (empty for a build with none); enabled reads the admin switch.
func New(running string, enabled func(context.Context) bool, log *slog.Logger) *Checker {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyFromEnvironment
	hc := &http.Client{
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are not followed")
		},
	}
	return &Checker{url: LatestURL, hc: hc, running: running, enabled: enabled, log: log,
		first: firstDelay, every: checkEvery, limit: callTimeout, soon: make(chan struct{}, 1)}
}

// Newer returns the plain version of a release newer than the running one,
// or empty. It is empty whenever the switch is off.
func (c *Checker) Newer(ctx context.Context) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	latest := c.latest
	c.mu.Unlock()
	// The database is read only when there is something to hide.
	if latest == "" || !c.enabled(ctx) {
		return ""
	}
	return latest
}

// Run checks after the first delay and then on every interval until ctx
// ends. A build with no release version never checks.
func (c *Checker) Run(ctx context.Context) {
	if _, ok := parse(c.running); !ok {
		return
	}
	t := time.NewTimer(c.first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.soon:
			c.checkOnce(ctx)
		case <-t.C:
			c.checkOnce(ctx)
			t.Reset(c.every)
		}
	}
}

// CheckSoon asks Run for a check now, for when an admin turns the switch
// on. It never waits, and a request already waiting covers this one.
func (c *Checker) CheckSoon() {
	select {
	case c.soon <- struct{}{}:
	default:
	}
}

// checkOnce asks GitHub when the switch is on. A failure keeps the last
// good answer and logs one line; users never see it as an error.
func (c *Checker) checkOnce(ctx context.Context) {
	if !c.enabled(ctx) {
		return
	}
	tag, err := c.fetch(ctx)
	if err != nil {
		c.log.Warn("could not check GitHub for a newer Foreman release", "error", err)
		return
	}
	newer, _ := Newer(tag, c.running)
	c.mu.Lock()
	c.latest = newer
	c.mu.Unlock()
}

func (c *Checker) fetch(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.limit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Foreman/"+clean(c.running))
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxBody {
		return "", errors.New("the reply is too large")
	}
	var doc struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("the reply is not usable JSON: %w", err)
	}
	return doc.TagName, nil
}

// clean keeps only the characters the Makefile allows in a version.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("._+-", r):
			return r
		}
		return -1
	}, s)
}

// parse reads major.minor.patch from a version such as "1.2.0", "v1.2.0"
// or "v1.2.0-5-gabc123" (git describe), ignoring anything after the patch.
func parse(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimPrefix(clean(strings.TrimSpace(s)), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		if p == "" || len(p) > 9 || strings.Trim(p, "0123456789") != "" {
			return out, false
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out, true
}

// Newer gives the plain version of tag ("1.3.0") when it is strictly newer
// than running. Either side unparseable gives false.
func Newer(tag, running string) (string, bool) {
	a, ok := parse(tag)
	if !ok {
		return "", false
	}
	b, ok := parse(running)
	if !ok {
		return "", false
	}
	for i := range a {
		if a[i] != b[i] {
			if a[i] > b[i] {
				return fmt.Sprintf("%d.%d.%d", a[0], a[1], a[2]), true
			}
			return "", false
		}
	}
	return "", false
}
