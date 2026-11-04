package game

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
)

const (
	// loadFailureReadLimit bounds the one-shot log read made after a game
	// stopped while loading, in time, lines and bytes.
	loadFailureReadLimit = 5 * time.Second
	maxLoadFailureLines  = 20000
	maxLoadFailureBytes  = 4 << 20
	// listedMods is how many mod names the message names.
	listedMods = 5
	// maxShownModName and maxErrorLine are the longest mod name and game
	// error line shown, in characters.
	maxShownModName = 100
	maxErrorLine    = 300
	// maxRecordedMods, maxRequiredVersions and maxVersionText bound what is
	// kept from one log: names per version, distinct versions, and the
	// length of a version.
	maxRecordedMods     = 1000
	maxRequiredVersions = 16
	maxVersionText      = 32
	// maxKeptText bounds a name or line kept before it is redacted and cut.
	maxKeptText = 4096
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
	mod     string // the last mod bullet seen, until its reason
	current string
	// byRequired lists the mods by the version they were made for.
	byRequired map[string][]string
	errLine    string
}

func (f *loadFailure) add(raw string) {
	line := strings.TrimSpace(secondsPrefix.ReplaceAllString(strings.TrimSuffix(raw, "\r"), ""))
	if m := modBullet.FindStringSubmatch(line); m != nil {
		f.mod = cut(m[1], maxKeptText)
		return
	}
	if m := incompatibleLine.FindStringSubmatch(line); m != nil {
		f.addIncompatible(cut(m[1], maxVersionText), cut(m[2], maxVersionText))
		return
	}
	if m := errorLine.FindStringSubmatch(line); m != nil {
		f.errLine = cut(m[1], maxKeptText)
	}
}

// addIncompatible records the mod of the last bullet, if any, as made for
// the required version. A mod without a bullet is still counted.
func (f *loadFailure) addIncompatible(current, required string) {
	mod := f.mod
	f.mod = ""
	if f.byRequired == nil {
		f.byRequired = map[string][]string{}
	}
	names, known := f.byRequired[required]
	if !known && len(f.byRequired) >= maxRequiredVersions {
		return
	}
	f.current = current
	if len(names) >= maxRecordedMods || (mod != "" && slices.Contains(names, mod)) {
		return
	}
	f.byRequired[required] = append(names, mod)
}

// reason words why the game stopped, as the end of the start failure's
// message, or returns "" when the log said nothing it may show. redact is
// the page-safe masking of a log line (SPEC 8.6, 14.8); every piece taken
// from the log goes through it before it is cut to length.
func (f *loadFailure) reason(redact func(string) string) string {
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
		out := fmt.Sprintf(": %s made for Factorio %s, and this game runs %s",
			count, redact(required), redact(f.current))
		if shown := shownNames(names, redact); shown != "" {
			out += ": " + shown
		}
		return out
	}
	if f.errLine == "" {
		return ""
	}
	return ". Factorio said: " + strings.TrimSpace(cut(redact(f.errLine), maxErrorLine))
}

// shownNames names the first listedMods named mods, redacted and cut to
// length, and counts the rest.
func shownNames(names []string, redact func(string) string) string {
	var shown []string
	for _, n := range names {
		if n == "" || len(shown) >= listedMods {
			continue
		}
		n = redact(n)
		if r := []rune(n); len(r) > maxShownModName {
			n = string(r[:maxShownModName]) + "…"
		}
		shown = append(shown, n)
	}
	out := strings.Join(shown, ", ")
	if rest := len(names) - len(shown); out != "" && rest > 0 {
		out += fmt.Sprintf(" and %d more", rest)
	}
	return out
}

// cut keeps at most n characters of s.
func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// loadFailureReason reads the log of the container's run and returns the
// game's own reason it stopped while loading, as the end of the start
// failure's message, or "" when the log gives none or cannot be read. Every
// start makes a fresh container (ADR 0003), so its whole log is this run's.
func (c *Controller) loadFailureReason(ctx context.Context, w *worldRun, cid string) string {
	ctx, cancel := context.WithTimeout(ctx, loadFailureReadLimit)
	defer cancel()
	s, err := c.docker.Logs(ctx, cid, docker.LogOptions{})
	if err != nil {
		return ""
	}
	defer func() { _ = s.Close() }()
	var f loadFailure
	read := 0
	for lines := 0; lines < maxLoadFailureLines && read < maxLoadFailureBytes; lines++ {
		l, err := s.Next()
		if err != nil {
			break
		}
		read += len(l.Text) + 1
		f.add(l.Text)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return f.reason(w.redact)
}
