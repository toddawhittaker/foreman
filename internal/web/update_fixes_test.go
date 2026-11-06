package web

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/releases"
	"github.com/toddawhittaker/foreman/internal/world"
)

// fixReleases answers each call on its own, so one can fail alone.
type fixReleases struct {
	versions   []string
	latest     releases.Latest
	verr, lerr error
}

func (f *fixReleases) Versions(context.Context) ([]string, error) {
	return f.versions, f.verr
}
func (f *fixReleases) Latest(context.Context) (releases.Latest, error) { return f.latest, f.lerr }

func (f *fixReleases) ImageSize(context.Context, string) (int64, error) { return 0, errFixHub }

var errFixHub = errors.New("docker hub down")

// SPEC 10.7: the picker's list comes from the image versions alone; a
// failed latest-release call only drops the labels.
func TestPickerNeedsOnlyVersions(t *testing.T) {
	s := &server{releases: &fixReleases{versions: []string{"2.0.1", "2.0.2"}, lerr: errFixHub}}
	p := s.versionPicker(context.Background(), "2.0.1")
	if p.Unreachable || !slices.Equal(p.Versions, []string{"2.0.2"}) || p.Stable != "" {
		t.Errorf("picker %+v: want 2.0.2 listed, reachable, no labels", p)
	}
	s = &server{releases: &fixReleases{verr: errFixHub, latest: releases.Latest{Stable: "2.0.2"}}}
	if p := s.versionPicker(context.Background(), ""); !p.Unreachable || len(p.Versions) != 0 {
		t.Errorf("picker %+v: want unreachable and empty", p)
	}
}

// SPEC 10.2: the notice is unreachable only when a call it needs fails.
func TestReleaseNoticeJudgesEachCall(t *testing.T) {
	sum := world.Summary{ID: "base", GameVersion: "2.0.1"}
	cases := []struct {
		name             string
		rel              *fixReleases
		unreachable      bool
		stable, exp, upd string
	}{
		{"latest fails", &fixReleases{versions: []string{"2.0.2"}, lerr: errFixHub}, true, "", "", ""},
		{"versions fail, nothing newer", &fixReleases{verr: errFixHub, latest: releases.Latest{Stable: "2.0.1"}}, false, "", "", ""},
		{"versions fail, newer out", &fixReleases{verr: errFixHub, latest: releases.Latest{Stable: "2.0.2"}}, true, "", "", ""},
		{"no stable known", &fixReleases{versions: []string{"2.1.0"}, latest: releases.Latest{Experimental: "2.1.0"}}, false, "", "2.1.0",
			"/worlds/base/update?game_version=2.1.0"},
		{"newer without image", &fixReleases{versions: []string{"2.0.1"}, latest: releases.Latest{Stable: "2.0.2"}}, false, "2.0.2", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := (&server{releases: c.rel}).releaseNotice(context.Background(), sum)
			if p.Unreachable != c.unreachable || p.Stable != c.stable || p.Experimental != c.exp || p.UpdatePath != c.upd {
				t.Errorf("notice %+v", p)
			}
		})
	}
}

// SPEC 10.3, 7.4: when the start after the version change was refused, the
// world is already on the new version, so the box offers a start anyway.
func TestRefusedStartAfterUpdateOffersStartAnyway(t *testing.T) {
	refused := func(worldVersion string) string {
		return controlHTML(t, serverCase(game.StateStopped, func(p *ServerPage) {
			p.World.GameVersion = worldVersion
			err := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemModMissing,
				Text: "a.zip uses m, but this world has no file for it.", Mods: []string{"m"}, StartAnyway: true}}}}
			p.Result, p.HaveResult = actionResult{Action: game.AuditUpdate, Err: err, Version: "2.1.30"}, true
		}))
	}
	after := refused("2.1.30")
	if !strings.Contains(after, ">Start anyway</button>") || !strings.Contains(after, `action="/worlds/base/start"`) ||
		strings.Contains(after, "Update anyway") {
		t.Errorf("refused after the change: want Start anyway:\n%s", after)
	}
	if before := refused("2.1.24"); !strings.Contains(before, ">Update anyway</button>") {
		t.Errorf("refused before the change: want Update anyway:\n%s", before)
	}
}

// DESIGN 7.5: the refusal box takes focus, unless the field summary shows.
func TestUpdateAndRollbackErrorTakesFocus(t *testing.T) {
	sum := world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.1"}
	focused := `id="action-error" tabindex="-1" autofocus`
	pages := []struct {
		name  string
		page  Page
		focus bool
	}{
		{"update refusal", UpdatePage{World: sum, Refusal: i18n.M("server.update.not_active")}, true},
		{"update refusal with field errors", UpdatePage{World: sum, Refusal: i18n.M("server.update.not_active"), VersionError: i18n.M("server.error.version_not_newer")}, false},
		{"rollback error", RollbackPage{World: sum, Error: i18n.M("server.error.no_rollback")}, true},
	}
	for _, c := range pages {
		t.Run(c.name, func(t *testing.T) {
			html := pageHTML(t, c.page)
			if !strings.Contains(html, `id="action-error" tabindex="-1"`) || strings.Contains(html, focused) != c.focus {
				t.Errorf("focus %v wanted:\n%s", c.focus, html)
			}
		})
	}
}

func noticeHTML(t *testing.T, p ReleaseNotice) string {
	t.Helper()
	var b strings.Builder
	if err := newTestRenderer(t).Fragment(&b, i18n.English, p, "release-notice"); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// SPEC 10.2 and DESIGN 7.5: a release without an image has only the
// sentence, no dead button; only news of a newer release is announced.
func TestReleaseNoticeMarkup(t *testing.T) {
	noImage := noticeHTML(t, ReleaseNotice{Current: "2.0.1", Stable: "2.0.2"})
	if strings.Contains(noImage, "<button") || !strings.Contains(noImage, "No image yet") {
		t.Errorf("no image: want only the sentence:\n%s", noImage)
	}
	if !strings.Contains(noImage, `aria-live="polite"`) {
		t.Errorf("newer release not announced:\n%s", noImage)
	}
	for _, p := range []ReleaseNotice{{Current: "2.0.1"}, {Current: "2.0.1", Unreachable: true}} {
		if html := noticeHTML(t, p); strings.Contains(html, "aria-live") {
			t.Errorf("%+v announced:\n%s", p, html)
		}
	}
}
