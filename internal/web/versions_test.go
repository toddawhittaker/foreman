package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/releases"
)

// Tests for the version picker and the release notice, the two fragments
// that call Docker Hub and factorio.com (SPEC 10.2, 10.7, 14.10).

const hostile = `<script>alert(1)</script>`
const hostileAttr = `2.1.99"><img src=x onerror=alert(1)>`

func wantNoMarkup(t *testing.T, what, body string) {
	t.Helper()
	for _, bad := range []string{"<script>alert", "<img src=x", `" onerror`} {
		if strings.Contains(body, bad) {
			t.Errorf("%s has %q:\n%s", what, bad, body)
		}
	}
}

func optionValues(body string) []string {
	var out []string
	for _, part := range strings.Split(body, `<option value="`)[1:] {
		out = append(out, part[:strings.Index(part, `"`)])
	}
	return out
}

func pickerRel() *fakeReleases {
	return &fakeReleases{
		versions: []string{"2.1.25", "2.1.24", "2.0.77", "2.0.76", "1.1.110"},
		latest:   releases.Latest{Stable: "2.0.77", Experimental: "2.1.25"},
	}
}

// SPEC 10.7: the picker lists only versions above the one asked, newest
// first as the source gave them, and marks the stable and experimental
// releases.
func TestVersionsListsOnlyVersionsAbove(t *testing.T) {
	g := newUpdateRig(t, pickerRel())

	body := g.get(versionsPath("2.0.77"))
	if got := strings.Join(optionValues(body), " "); got != "2.1.25 2.1.24" {
		t.Errorf("above 2.0.77 lists %q:\n%s", got, body)
	}
	g.wantContains("picker", body, `>2.1.25 (experimental)<`)
	g.wantMissing("picker", body, "stable", "2.0.76", "1.1.110")

	body = g.get(versionsPath("2.0.76"))
	if got := strings.Join(optionValues(body), " "); got != "2.1.25 2.1.24 2.0.77" {
		t.Errorf("above 2.0.76 lists %q:\n%s", got, body)
	}
	g.wantContains("picker", body, `>2.0.77 (stable)<`, `>2.1.25 (experimental)<`)

	// Nothing is above the newest, and the list is still a good answer.
	rep := g.do(http.MethodGet, versionsPath("2.1.25"), nil, http.Header{"Cookie": {g.cookie.String()}})
	if rep.StatusCode != http.StatusOK || len(optionValues(rep.body)) != 0 {
		t.Errorf("above the newest: %d\n%s", rep.StatusCode, rep.body)
	}

	// No "above" lists every version, as the import form needs.
	if got := len(optionValues(g.get(PathVersions))); got != 5 {
		t.Errorf("no above lists %d versions, want 5", got)
	}
}

// SPEC 10.7: when Docker Hub cannot be reached the page still works: the list
// is empty, the answer is 200, and the error's words (which may hold an
// address) are not shown. The same holds with no release source at all.
func TestVersionsShowsNothingWhenTheSourceFails(t *testing.T) {
	leak := errors.New("dial tcp 10.9.8.7:443: secret-hub-detail")
	for name, rel := range map[string]*fakeReleases{
		"versions fail": {versionsErr: leak, latest: releases.Latest{Stable: "2.1.25"}},
		"both fail":     {versionsErr: leak, latestErr: leak},
		"no source":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			g := newUpdateRig(t, rel)
			rep := g.do(http.MethodGet, versionsPath("2.0.77"), nil, http.Header{"Cookie": {g.cookie.String()}})
			if rep.StatusCode != http.StatusOK {
				t.Fatalf("status %d\n%s", rep.StatusCode, rep.body)
			}
			if got := optionValues(rep.body); len(got) != 0 {
				t.Errorf("lists %q", got)
			}
			g.wantMissing("picker", rep.body, "secret-hub-detail", "10.9.8.7")
		})
	}
}

// With the versions known but the latest releases not, the list still shows
// and claims nothing about which release is stable or experimental.
func TestVersionsWithoutLatestReleasesMarksNothing(t *testing.T) {
	rel := pickerRel()
	rel.latestErr = errHub
	g := newUpdateRig(t, rel)
	rep := g.do(http.MethodGet, versionsPath("2.0.77"), nil, http.Header{"Cookie": {g.cookie.String()}})
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("status %d", rep.StatusCode)
	}
	g.wantMissing("picker", rep.body, "stable", "experimental")
}

// SPEC 14.10: text from Docker Hub or factorio.com is data, never markup, and
// neither is the "above" the browser sends.
func TestVersionsNeverRendersHostileText(t *testing.T) {
	rel := &fakeReleases{
		versions: []string{hostile, hostileAttr, "2.1.25"},
		latest:   releases.Latest{Stable: hostile, Experimental: hostileAttr},
	}
	g := newUpdateRig(t, rel)
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.addImage(newVersion)
	header := http.Header{"Cookie": {g.cookie.String()}}

	for _, p := range []string{PathVersions, versionsPath("2.0.1"), releasesURL(id), PathVersions + "?above=" + url.QueryEscape(hostile)} {
		rep := g.do(http.MethodGet, p, nil, header)
		if rep.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", p, rep.StatusCode)
		}
		wantNoMarkup(t, p, rep.body)
	}
	g.wantContains("picker", g.get(versionsPath("2.0.1")), `value="2.1.25"`)
}

// SPEC 10.2: a newer release is shown with a link to the wiki's version
// history of its line; its Update link goes to the Update page of this world.
func TestReleaseNoticeOffersTheNewRelease(t *testing.T) {
	rel := &fakeReleases{versions: []string{"2.1.25", oldVersion}, latest: releases.Latest{Stable: "2.1.25"}}
	g := newUpdateRig(t, rel)
	id := g.newWorld("Alpha", fixtureNoSA, false)

	body := g.get(releasesURL(id))
	g.wantContains("notice", body, "2.1.25", "stable", `https://wiki.factorio.com/Version_history/2.1.0`, `href="`+updateURL(id))
	g.wantMissing("notice", body, "No image yet", "cannot be reached")
}

// SPEC 10.2: a release with no image on Docker Hub yet says so and offers no
// Update link; the button is off.
func TestReleaseNoticeWithNoImageYet(t *testing.T) {
	rel := &fakeReleases{versions: []string{oldVersion}, latest: releases.Latest{Stable: "2.1.26"}}
	g := newUpdateRig(t, rel)
	id := g.newWorld("Alpha", fixtureNoSA, false)

	body := g.get(releasesURL(id))
	g.wantContains("notice", body, "2.1.26", "No image yet", "wiki.factorio.com/Version_history/2.1.0")
	g.wantMissing("notice", body, `href="`+updateURL(id))
}

// SPEC 10.2: when the world is up to date, or ahead of the stable release,
// there is nothing to update to and no Update link.
func TestReleaseNoticeWhenUpToDate(t *testing.T) {
	for name, latest := range map[string]releases.Latest{
		"equal":                 {Stable: oldVersion},
		"world ahead of stable": {Stable: "2.0.77"},
		"nothing published":     {},
	} {
		t.Run(name, func(t *testing.T) {
			g := newUpdateRig(t, &fakeReleases{versions: []string{oldVersion, "2.0.77"}, latest: latest})
			id := g.newWorld("Alpha", fixtureNoSA, false)
			body := g.get(releasesURL(id))
			g.wantContains("notice", body, "newest Factorio release")
			g.wantMissing("notice", body, "/update", "No image yet", "cannot be reached")
		})
	}
}

// SPEC 10.2: when Docker Hub cannot be reached the notice says so, offers no
// Update link, and shows nothing of the error. A failed version list means
// no image can be promised, so there is no link then either.
func TestReleaseNoticeWhenTheSourceFails(t *testing.T) {
	leak := errors.New("Get https://hub.docker.com: secret-hub-detail")
	for name, rel := range map[string]*fakeReleases{
		"latest fails":   {versions: []string{"2.1.25"}, latestErr: leak},
		"versions fail":  {versionsErr: leak, latest: releases.Latest{Stable: "2.1.25"}},
		"both fail":      {versionsErr: leak, latestErr: leak},
		"no source set":  nil,
		"hostile latest": {versions: []string{"2.1.25"}, latest: releases.Latest{Stable: hostile}},
	} {
		t.Run(name, func(t *testing.T) {
			g := newUpdateRig(t, rel)
			id := g.newWorld("Alpha", fixtureNoSA, false)
			rep := g.do(http.MethodGet, releasesURL(id), nil, http.Header{"Cookie": {g.cookie.String()}})
			if rep.StatusCode != http.StatusOK {
				t.Fatalf("status %d\n%s", rep.StatusCode, rep.body)
			}
			g.wantMissing("notice", rep.body, `href="`+updateURL(id), "secret-hub-detail")
			wantNoMarkup(t, "notice", rep.body)
			if rel != nil && rel.latestErr != nil {
				g.wantContains("notice", rep.body, "Docker Hub cannot be reached")
			}
		})
	}
}
