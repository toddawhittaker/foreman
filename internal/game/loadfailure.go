package game

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/secret"
)

const (
	// loadFailureReadLimit bounds the one-shot log read made after a game
	// stopped while loading.
	loadFailureReadLimit = 5 * time.Second
	// listedMods is how many mod names the message names.
	listedMods = 5
	// maxErrorLine is the longest game error line shown, in characters.
	maxErrorLine = 300
	// maxRecordedMods bounds the names kept from one log.
	maxRecordedMods = 1000
)

var (
	// "Error Util.cpp:81: message", after the game's seconds prefix.
	errorLine = regexp.MustCompile(`^Error [A-Za-z]+\.cpp:\d+: (.*)$`)
	// The game lists a mod it cannot load as a bullet line.
	modBullet = regexp.MustCompile(`^•\s+(\S+)$`)
	// The reason under that bullet.
	incompatibleLine = regexp.MustCompile(`Incompatible Factorio version \(current: ([0-9][0-9.]*), required: ([0-9][0-9.]*)\)`)
)

// loadFailure reads the game's log lines after it stopped while loading,
// and keeps only what the patterns above match, never any other line
// (SPEC 14.8).
type loadFailure struct {
	mod     string // the last mod bullet seen
	current string
	// byRequired lists the mods by the version they were made for.
	byRequired map[string][]string
	errLine    string
}

func (f *loadFailure) add(raw string) {
	line := strings.TrimSpace(secondsPrefix.ReplaceAllString(strings.TrimSuffix(raw, "\r"), ""))
	if m := modBullet.FindStringSubmatch(line); m != nil {
		f.mod = m[1]
		return
	}
	if m := incompatibleLine.FindStringSubmatch(line); m != nil {
		if f.byRequired == nil {
			f.byRequired = map[string][]string{}
		}
		f.current = m[1]
		names := f.byRequired[m[2]]
		if len(names) < maxRecordedMods && !slices.Contains(names, f.mod) {
			f.byRequired[m[2]] = append(names, f.mod)
		}
		return
	}
	if m := errorLine.FindStringSubmatch(line); m != nil {
		f.errLine = m[1]
	}
}

// reason words why the game stopped, or returns "" when the log said
// nothing it may show. Each piece taken from the log is redacted here.
func (f *loadFailure) reason(secrets []secret.Secret) string {
	if len(f.byRequired) > 0 {
		var required string
		for r, names := range f.byRequired {
			if required == "" || len(names) > len(f.byRequired[required]) ||
				(len(names) == len(f.byRequired[required]) && r < required) {
				required = r
			}
		}
		names := f.byRequired[required]
		count := fmt.Sprintf("%d mods were", len(names))
		if len(names) == 1 {
			count = "1 mod was"
		}
		out := fmt.Sprintf("%s made for Factorio %s, and this game runs %s",
			count, Redact(required, secrets...), Redact(f.current, secrets...))
		if shown := shownNames(names, secrets); shown != "" {
			out += ": " + shown
		}
		return out
	}
	if f.errLine == "" {
		return ""
	}
	line := f.errLine
	if r := []rune(line); len(r) > maxErrorLine {
		line = string(r[:maxErrorLine])
	}
	return strings.TrimSpace(Redact(line, secrets...))
}

// shownNames names the first listedMods mods, redacted, and counts the rest.
func shownNames(names []string, secrets []secret.Secret) string {
	var shown []string
	for _, n := range names {
		if n != "" && len(shown) < listedMods {
			shown = append(shown, Redact(n, secrets...))
		}
	}
	out := strings.Join(shown, ", ")
	if rest := len(names) - len(shown); out != "" && rest > 0 {
		out += fmt.Sprintf(" and %d more", rest)
	}
	return out
}

// loadFailureReason reads the log of the container's run since the given
// time and returns the game's own reason it stopped while loading, or ""
// when the log gives none or cannot be read.
func (c *Controller) loadFailureReason(ctx context.Context, w *worldRun, cid string, since time.Time) string {
	ctx, cancel := context.WithTimeout(ctx, loadFailureReadLimit)
	defer cancel()
	s, err := c.docker.Logs(ctx, cid, docker.LogOptions{Since: since})
	if err != nil {
		return ""
	}
	defer func() { _ = s.Close() }()
	var f loadFailure
	for {
		l, err := s.Next()
		if err != nil {
			break
		}
		f.add(l.Text)
	}
	w.mu.Lock()
	secrets := w.secrets
	w.mu.Unlock()
	return f.reason(secrets)
}
