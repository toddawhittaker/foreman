package game

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
)

const (
	// loadFailureReadLimit bounds the one-shot log read made after a game
	// stopped while loading, in time and in lines; the newest lines are
	// read, and of those only the newest maxLoadFailureBytes are kept.
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
	// errLine is the last error line of the run being read, and lastErr
	// that of the run before it, for when this one has said none yet.
	errLine, lastErr string
	// kind is the catalogue's name for the run being read; an older run's
	// is never shown, so no match means unknown (SPEC 8.7).
	kind string
	// runError is the reason the run being read gave for a failed load.
	runError string
	// failed is whether the newest run to finish loading failed to, and
	// failError the reason that run gave (SPEC 8.7).
	failed    bool
	failError string
}

// Lines are not cut before they are masked, so a cut cannot leave part of
// a secret; Docker's reader already bounds a line's length.
func (f *loadFailure) add(raw string) {
	switch ev := ParseLine(raw); ev.Kind {
	case KindLoadStarted:
		// Every run begins here, so an older run's words are not its reason.
		if f.errLine != "" {
			f.lastErr = f.errLine
		}
		f.errLine, f.runError, f.kind = "", "", ""
	case KindInGame:
		f.failed, f.failError = false, ""
	case KindLoadError:
		f.runError = ev.Detail
	case KindLoadFailed:
		f.failed, f.failError = true, f.runError
	}
	line := strings.TrimSpace(secondsPrefix.ReplaceAllString(strings.TrimSuffix(raw, "\r"), ""))
	f.kind = crashreasons.Prefer(f.kind, crashreasons.Line(line))
	if m := modBullet.FindStringSubmatch(line); m != nil {
		f.mod = cut(m[1], maxKeptText)
		return
	}
	if m := incompatibleLine.FindStringSubmatch(line); m != nil {
		f.addIncompatible(cut(m[1], maxVersionText), cut(m[2], maxVersionText))
		return
	}
	if m := errorLine.FindStringSubmatch(line); m != nil {
		f.errLine = m[1]
	}
}

// addIncompatible records the mod of the last bullet, if any, as made for
// the required version. A mod without a bullet is still counted, once: the
// log holds every run of this start, and the same nameless line from each
// run must not raise the count.
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
	if len(names) >= maxRecordedMods || slices.Contains(names, mod) {
		return
	}
	f.byRequired[required] = append(names, mod)
}

// reason words why the game stopped, as the end of the start failure's
// message, or returns "" when the log said nothing it may show. redact is
// the page-safe masking of a log line (SPEC 8.6, 14.8); every piece taken
// from the log goes through it before it is cut to length.
func (f *loadFailure) reason(redact func(string) string) LoadReason {
	if len(f.byRequired) > 0 {
		var required string
		for r, names := range f.byRequired {
			if required == "" || len(names) > len(f.byRequired[required]) ||
				(len(names) == len(f.byRequired[required]) && r < required) {
				required = r
			}
		}
		names := f.byRequired[required]
		r := LoadReason{Kind: crashreasons.ModsForOtherVersion, Mods: len(names), Required: redact(required), Current: redact(f.current)}
		r.Shown, r.More = shownNames(names, redact)
		return r
	}
	said := f.errLine
	if said == "" {
		said = f.lastErr
	}
	r := LoadReason{Kind: f.kind}
	if said != "" {
		r.Said = shownText(redact, said)
	}
	return r
}

// shownText is the game's words from its log as a page may show them:
// masked first, so the cut cannot leave part of a secret, then cut to
// maxErrorLine (SPEC 8.6, 14.8).
func shownText(redact func(string) string, s string) string {
	return strings.TrimSpace(cut(redact(s), maxErrorLine))
}

// LoadReason is the game's own reason it stopped while loading, from its
// log (SPEC 8.7): mods made for another Factorio, or else the game's error
// line, Said. Every piece is masked like a log line. The zero value means
// the log said nothing it may show.
type LoadReason struct {
	// Kind is the crashreasons id for the reason, "" when unknown. Out of
	// memory comes from Docker's OOMKilled flag, never from the log.
	Kind string
	// Mods were made for Factorio Required; the game runs Current. Shown
	// names the first few, and More counts the rest.
	Mods              int
	Required, Current string
	Shown             []string
	More              int
	Said              string
}

// IsZero reports whether the log gave no reason.
func (r LoadReason) IsZero() bool { return r.Mods == 0 && r.Said == "" && r.Kind == "" }

// text is the reason as the end of an English sentence, for the log.
func (r LoadReason) text() string {
	if r.Mods > 0 {
		count := fmt.Sprintf("%d mods were", r.Mods)
		if r.Mods == 1 {
			count = "1 mod was"
		}
		out := fmt.Sprintf(": %s made for Factorio %s, and this game runs %s", count, r.Required, r.Current)
		if len(r.Shown) > 0 {
			out += ": " + strings.Join(r.Shown, ", ")
			if r.More > 0 {
				out += fmt.Sprintf(" and %d more", r.More)
			}
		}
		return out
	}
	if r.Kind == crashreasons.OutOfMemory {
		return ": it ran out of memory"
	}
	if r.Said == "" {
		return ""
	}
	return ". Factorio said: " + r.Said
}

// shownNames is the first listedMods named mods, redacted and cut to
// length, and how many are left out.
func shownNames(names []string, redact func(string) string) ([]string, int) {
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
	return shown, len(names) - len(shown)
}

// cut keeps at most n characters of s.
func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// stoppedWhileLoading reads the log of the container's run after the game
// stopped while loading and words why (SPEC 7.3, 8.7). The log decides,
// not what the follower has read so far: the container can stop, and
// Docker restart it, before the follower reaches the failure line, and a
// restart's first line begins a new run. Every start makes a fresh
// container (ADR 0003), so its log holds every run of this start, Docker's
// restarts after a crash included, and nothing older. A failed load is
// also marked on the run, as the follower would, so the world's state
// agrees with the error.
func (c *Controller) stoppedWhileLoading(ctx context.Context, w *worldRun, cid string) *StartError {
	f := c.readLoadLog(ctx, cid)
	oom := c.oomKilled(ctx, cid)
	w.mu.Lock()
	defer w.mu.Unlock()
	reason := f.reason(w.redact)
	if oom {
		reason.Kind = crashreasons.OutOfMemory
	}
	if !f.failed {
		return &StartError{Kind: StartStopped, Reason: reason}
	}
	detail := shownText(w.redact, f.failError)
	if w.containerID == cid && !w.run.loadFailed {
		w.run.loadFailed, w.run.inGame, w.run.loadError = true, false, detail
		c.changedLocked(w, true)
	}
	return &StartError{Kind: StartLoadFailed, Detail: detail, Reason: reason}
}

// oomKilled reports whether Docker says the container's last exit was the
// kernel's out-of-memory kill. Docker clears the flag when it restarts the
// container, so a kill it has already restarted reads false.
func (c *Controller) oomKilled(ctx context.Context, cid string) bool {
	ctx, cancel := context.WithTimeout(ctx, loadFailureReadLimit)
	defer cancel()
	cont, err := c.docker.InspectContainer(ctx, cid)
	return err == nil && cont.State.OOMKilled
}

// readLoadLog reads the newest lines of the container's log once, within
// the limits above, and keeps the newest of them, so the last run's lines
// are never the ones left out. A read that fails or ends early gives what
// was read before it.
func (c *Controller) readLoadLog(ctx context.Context, cid string) loadFailure {
	ctx, cancel := context.WithTimeout(ctx, loadFailureReadLimit)
	defer cancel()
	var kept []string
	if s, err := c.docker.Logs(ctx, cid, docker.LogOptions{Tail: maxLoadFailureLines}); err == nil {
		size := 0
		for range maxLoadFailureLines {
			l, err := s.Next()
			if err != nil {
				break
			}
			kept = append(kept, l.Text)
			size += len(l.Text) + 1
			for size > maxLoadFailureBytes {
				size -= len(kept[0]) + 1
				kept = kept[1:]
			}
		}
		_ = s.Close()
	}
	var f loadFailure
	for _, l := range kept {
		f.add(l)
	}
	return f
}
