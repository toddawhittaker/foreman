package modportal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func TestModRecorded(t *testing.T) {
	_, c, _ := fakePortal(t)
	m, err := c.Mod(context.Background(), "visible-planets")
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "visible-planets" || m.Title != "Visible Planets in Space" || m.Deprecated ||
		m.Downloads != 155092 || m.Summary == "" {
		t.Errorf("unexpected mod %+v", m)
	}
	if len(m.Releases) != 10 {
		t.Fatalf("%d releases, want 10", len(m.Releases))
	}
	for i := 1; i < len(m.Releases); i++ {
		if world.CompareVersions(m.Releases[i-1].Version, m.Releases[i].Version) >= 0 {
			t.Errorf("releases not oldest first at %d: %s then %s", i, m.Releases[i-1].Version, m.Releases[i].Version)
		}
	}
	last := m.Releases[len(m.Releases)-1]
	want := Release{
		Version:         "1.8.2",
		FileName:        "visible-planets_1.8.2.zip",
		DownloadURL:     "/download/visible-planets/6a3faa7df92afce66cc72aed",
		SHA1:            "a519a4075e252eae84059e07edb20313bd34b3fa",
		FactorioVersion: "2.1",
		Dependencies:    []string{"base >= 2.1", "space-age >= 2.1", "?PlanetsLib >= 1.19.0"},
	}
	if fmt.Sprint(last) != fmt.Sprint(want) {
		t.Errorf("last release\n got %+v\nwant %+v", last, want)
	}
}

func TestModDeprecated(t *testing.T) {
	_, c, _ := fakePortal(t)
	m, err := c.Mod(context.Background(), "chest-auto-sort")
	if err != nil || !m.Deprecated {
		t.Errorf("deprecated mod: %+v, %v", m, err)
	}
}

func TestModIncompatibilitiesAndOptionalKeptRaw(t *testing.T) {
	_, c, _ := fakePortal(t)
	m, err := c.Mod(context.Background(), "Krastorio2")
	if err != nil {
		t.Fatal(err)
	}
	deps := m.Releases[len(m.Releases)-1].Dependencies
	for _, want := range []string{"base >= 2.1.22", "+ ChangeInserterDropLane >= 1.3.0", "(?) Flow Control >= 3.0.5", "! Krastorio", "! Power Armor MK3"} {
		if !slices.Contains(deps, want) {
			t.Errorf("dependency %q missing from %v", want, deps)
		}
	}
}

func TestModNameWithSpaceIsEscaped(t *testing.T) {
	var raw string
	c := handlerClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.EscapedPath()
		http.NotFound(w, r)
	})
	_, _ = c.Mod(context.Background(), "Flow Control")
	if raw != "/api/mods/Flow%20Control/full" {
		t.Errorf("path was %q", raw)
	}
	_, c2, _ := fakePortal(t)
	m, err := c2.Mod(context.Background(), "Flow Control")
	if err != nil || m.Name != "Flow Control" {
		t.Errorf("%+v, %v", m, err)
	}
}

func TestModNotOnPortal(t *testing.T) {
	p, c, _ := fakePortal(t)
	if _, err := c.Mod(context.Background(), "no-such-mod"); !errors.Is(err, ErrNotOnPortal) {
		t.Errorf("got %v, want ErrNotOnPortal", err)
	}
	// A server error is not "not on the portal".
	p.FailAPI(http.StatusBadGateway)
	_, err := c.Mod(context.Background(), "flib")
	if !errors.Is(err, ErrUnreachable) || errors.Is(err, ErrNotOnPortal) {
		t.Errorf("got %v, want ErrUnreachable", err)
	}
}

func TestModBadNamesAskNothing(t *testing.T) {
	p, c, _ := fakePortal(t)
	for _, name := range []string{"", ".", "..", "../x", "a/b", `a\b`, ".hidden", "a\x00b", "a\nb"} {
		if _, err := c.Mod(context.Background(), name); !errors.Is(err, ErrNotOnPortal) {
			t.Errorf("%q: got %v", name, err)
		}
	}
	if p.FullCalls("") != 0 || p.FullCalls("..") != 0 {
		t.Error("a bad name reached the portal")
	}
}

func TestModReplyCap(t *testing.T) {
	c := handlerClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"big","summary":"` + strings.Repeat("a", maxFull) + `"}`))
	})
	if _, err := c.Mod(context.Background(), "big"); !errors.Is(err, ErrUnreachable) {
		t.Errorf("got %v, want ErrUnreachable", err)
	}
}

func TestModBadReply(t *testing.T) {
	for name, body := range map[string]string{"not json": "<html>", "no name": `{"releases":[]}`} {
		c := handlerClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		if _, err := c.Mod(context.Background(), "x"); !errors.Is(err, ErrUnreachable) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestModTimesOut(t *testing.T) {
	c := handlerClient(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	c.callTimeout = 100 * time.Millisecond
	if _, err := c.Mod(context.Background(), "x"); !errors.Is(err, ErrUnreachable) {
		t.Errorf("got %v", err)
	}
}

func TestModCacheAndErrors(t *testing.T) {
	p, c, clk := fakePortal(t)
	ctx := context.Background()
	p.FailAPI(http.StatusInternalServerError)
	if _, err := c.Mod(ctx, "visible-planets"); err == nil {
		t.Fatal("expected an error")
	}
	p.FailAPI(0)
	if _, err := c.Mod(ctx, "visible-planets"); err != nil {
		t.Fatalf("an error was cached: %v", err)
	}
	if _, err := c.Mod(ctx, "visible-planets"); err != nil || p.FullCalls("visible-planets") != 2 {
		t.Fatalf("second success must come from the cache: %v, %d calls", err, p.FullCalls("visible-planets"))
	}
	// A 404 is an answer, kept as long as a mod is.
	if _, err := c.Mod(ctx, "nope"); !errors.Is(err, ErrNotOnPortal) {
		t.Fatal(err)
	}
	if _, err := c.Mod(ctx, "nope"); !errors.Is(err, ErrNotOnPortal) || p.FullCalls("nope") != 1 {
		t.Fatalf("a 404 was asked again: %d calls", p.FullCalls("nope"))
	}
	clk.Advance(15*time.Minute + time.Second)
	if _, err := c.Mod(ctx, "nope"); !errors.Is(err, ErrNotOnPortal) || p.FullCalls("nope") != 2 {
		t.Fatalf("a 404 did not expire after 15 minutes: %d calls", p.FullCalls("nope"))
	}
	_, _ = c.Mod(ctx, "Flow Control")
	clk.Advance(14*time.Minute + 59*time.Second)
	_, _ = c.Mod(ctx, "Flow Control")
	if p.FullCalls("Flow Control") != 1 {
		t.Fatal("expired early")
	}
	clk.Advance(2 * time.Second)
	_, _ = c.Mod(ctx, "Flow Control")
	if p.FullCalls("Flow Control") != 2 {
		t.Fatal("did not expire after 15 minutes")
	}
}

func TestModCacheIsNotShared(t *testing.T) {
	_, c, _ := fakePortal(t)
	m, _ := c.Mod(context.Background(), "visible-planets")
	m.Releases[0].Version = "changed"
	m.Releases[len(m.Releases)-1].Dependencies[0] = "changed"
	again, _ := c.Mod(context.Background(), "visible-planets")
	if again.Releases[0].Version == "changed" || again.Releases[len(again.Releases)-1].Dependencies[0] == "changed" {
		t.Error("a caller changed what the cache holds")
	}
}

func TestModsFourAtOnce(t *testing.T) {
	p, c, _ := fakePortal(t)
	p.FullDelay = 150 * time.Millisecond
	asked := []string{"flib", "jetpack", "Krastorio2", "Flow Control", "visible-planets", "chest-auto-sort", "missing-one", "missing-two", "flib"}
	got := c.Mods(context.Background(), asked)
	if len(got) != 8 {
		t.Fatalf("%d results, want 8", len(got))
	}
	for _, n := range []string{"Krastorio2", "Flow Control", "visible-planets", "chest-auto-sort"} {
		if got[n].Err != nil || got[n].Mod.Name != n {
			t.Errorf("%s: %+v", n, got[n])
		}
	}
	for _, n := range []string{"flib", "jetpack"} { // in the list, but no recorded /full
		if !errors.Is(got[n].Err, ErrNotOnPortal) {
			t.Errorf("%s: %v", n, got[n].Err)
		}
	}
	if !errors.Is(got["missing-one"].Err, ErrNotOnPortal) {
		t.Errorf("missing-one: %v", got["missing-one"].Err)
	}
	if p.FullCalls("flib") != 1 {
		t.Errorf("a repeated name was asked %d times", p.FullCalls("flib"))
	}
	if n := p.MaxConcurrentFull(); n != 4 {
		t.Errorf("%d /full requests ran at once, want 4", n)
	}
}

func TestModsLimitHoldsAcrossCallers(t *testing.T) {
	p, c, _ := fakePortal(t)
	p.FullDelay = 50 * time.Millisecond
	done := make(chan struct{})
	for i := 0; i < 12; i++ {
		go func() {
			_, _ = c.Mod(context.Background(), fmt.Sprintf("missing-%d", i))
			done <- struct{}{}
		}()
	}
	for i := 0; i < 12; i++ {
		<-done
	}
	if n := p.MaxConcurrentFull(); n > 4 {
		t.Errorf("%d /full requests ran at once", n)
	}
}

func TestModWaitingForASlotStopsWhenCancelled(t *testing.T) {
	c := handlerClient(t, func(http.ResponseWriter, *http.Request) {})
	for i := 0; i < 4; i++ {
		c.full <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Mod(ctx, "x"); !errors.Is(err, ErrUnreachable) || !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
}

func TestNothingIsFetchedAtConstruction(t *testing.T) {
	p, _, clk := fakePortal(t)
	clk.Advance(48 * time.Hour)
	if p.ListCalls() != 0 || p.FullCalls("flib") != 0 {
		t.Error("the client fetched without being asked")
	}
}

func TestParseModSortsReleases(t *testing.T) {
	m, err := parseMod([]byte(`{"name":"m","releases":[
		{"version":"1.0.10","file_name":"m_1.0.10.zip"},{"version":"1.0.2","file_name":"m_1.0.2.zip"},
		{"version":"","file_name":"m_.zip"},{"version":"0.9.0","file_name":"m_0.9.0.zip"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range m.Releases {
		got = append(got, r.Version)
	}
	if strings.Join(got, ",") != "0.9.0,1.0.2,1.0.10" {
		t.Errorf("got %v", got)
	}
}

// Plan ruling 11 for SPEC 14.10: a release whose file_name is not
// <name>_<version>.zip is not used, whatever part is wrong.
func TestParseModDropsReleasesNamedForAnotherMod(t *testing.T) {
	m, err := parseMod([]byte(`{"name":"m","releases":[
		{"version":"1.0.0","file_name":"m_1.0.0.zip"},
		{"version":"1.1.0","file_name":"other_1.1.0.zip"},
		{"version":"1.2.0","file_name":"m_1.2.1.zip"},
		{"version":"1.3.0","file_name":"m_1.3.0.tar"},
		{"version":"1.4.0"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Releases) != 1 || m.Releases[0].Version != "1.0.0" {
		t.Errorf("releases %+v, want only 1.0.0", m.Releases)
	}
}
