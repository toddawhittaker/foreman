package world

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var tidyBase = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// ages makes autosaves from numbers listed oldest first, a minute apart.
func ages(names ...string) []autosave {
	var out []autosave
	for i, name := range names {
		n, ok := autosaveNumber(name)
		if !ok {
			panic(name)
		}
		out = append(out, autosave{name: name, n: n, modTime: tidyBase.Add(time.Duration(i) * time.Minute)})
	}
	return out
}

func numbered(ns ...int) []string {
	var out []string
	for _, n := range ns {
		out = append(out, autosaveName(n))
	}
	return out
}

// result is where each file ends up: its new name, or "" when removed.
func result(t AutosaveTidy, saves []autosave) map[string]string {
	out := map[string]string{}
	for _, s := range saves {
		out[s.name] = t.NewName(s.name)
	}
	return out
}

func TestAutosaveNumber(t *testing.T) {
	good := map[string]int{"_autosave1.zip": 1, "_autosave20.zip": 20, "_autosave7.tidy.zip": 7}
	for name, want := range good {
		if n, ok := autosaveNumber(name); !ok || n != want {
			t.Errorf("%q = %d, %v", name, n, ok)
		}
	}
	for _, name := range []string{"_autosave.zip", "_autosave0.zip", "_autosave01.zip", "_autosave1.tmp.zip",
		"_autosave1a.zip", "autosave1.zip", "_autosave-1.zip", "base.zip", "_autosave1.zip.bak", ""} {
		if _, ok := autosaveNumber(name); ok {
			t.Errorf("%q read as an autosave", name)
		}
	}
}

func TestPlanAutosaveTidy(t *testing.T) {
	// The spec's example: 20 slots down to 10, autosaves written 18..20
	// then 1..17, so 17 is newest and K = 17.
	order := append(numbered(18, 19, 20), numbered(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17)...)
	spec := map[string]string{}
	for i, from := range []int{17, 16, 15, 14, 13, 12, 11, 10, 9, 8} {
		spec[autosaveName(from)] = autosaveName([]int{7, 6, 5, 4, 3, 2, 1, 10, 9, 8}[i])
	}
	for _, n := range []int{1, 2, 3, 4, 5, 6, 7, 18, 19, 20} {
		spec[autosaveName(n)] = ""
	}

	// A graceful stop re-saved the loaded file 3 after autosave 17.
	stopped := slices.DeleteFunc(slices.Clone(order), func(s string) bool { return s == autosaveName(3) })
	stopped = append(stopped, autosaveName(3))

	cases := []struct {
		name   string
		saves  []autosave
		slots  int
		loaded string
		want   map[string]string
		kept   int
	}{
		{"spec example", ages(order...), 10, "", spec, 10},
		{"spec example, loaded file known and not newest", ages(order...), 10, autosaveName(5), spec, 10},
		{
			// 3 is newest by time but was the loaded file; K = 17 still.
			"K skips the loaded file", ages(stopped...), 10, autosaveName(3),
			map[string]string{
				autosaveName(3): autosaveName(7), autosaveName(17): autosaveName(6), autosaveName(16): autosaveName(5),
				autosaveName(15): autosaveName(4), autosaveName(14): autosaveName(3), autosaveName(13): autosaveName(2),
				autosaveName(12): autosaveName(1), autosaveName(11): autosaveName(10), autosaveName(10): autosaveName(9),
				autosaveName(9): autosaveName(8), autosaveName(1): "", autosaveName(2): "", autosaveName(4): "",
				autosaveName(5): "", autosaveName(6): "", autosaveName(7): "", autosaveName(8): "",
				autosaveName(18): "", autosaveName(19): "", autosaveName(20): "",
			}, 10,
		},
		{
			"S does not divide: 7 down to 3, K = 7", ages(numbered(1, 2, 3, 4, 5, 6, 7)...), 3, "",
			map[string]string{
				autosaveName(7): autosaveName(1), autosaveName(6): autosaveName(3), autosaveName(5): autosaveName(2),
				autosaveName(1): "", autosaveName(2): "", autosaveName(3): "", autosaveName(4): "",
			}, 3,
		},
		{
			"S divides: 6 down to 3, K = 4", ages(numbered(5, 6, 1, 2, 3, 4)...), 3, "",
			map[string]string{
				autosaveName(4): autosaveName(1), autosaveName(3): autosaveName(3), autosaveName(2): autosaveName(2),
				autosaveName(1): "", autosaveName(5): "", autosaveName(6): "",
			}, 3,
		},
		{
			"fewer autosaves than S, with gaps", ages(numbered(2, 9, 12)...), 10, "",
			map[string]string{autosaveName(12): autosaveName(2), autosaveName(9): autosaveName(1), autosaveName(2): autosaveName(10)}, 3,
		},
		{
			// A tidy cut short after the first renames: 17 and 16 are
			// leftovers, read as autosaves 17 and 16.
			"leftovers from a tidy cut short",
			ages(append(append(numbered(18, 19, 20), numbered(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)...), autosaveTidyName(16), autosaveTidyName(17))...),
			10, "",
			func() map[string]string {
				m := maps.Clone(spec)
				m[autosaveTidyName(17)], m[autosaveTidyName(16)] = m[autosaveName(17)], m[autosaveName(16)]
				delete(m, autosaveName(17))
				delete(m, autosaveName(16))
				return m
			}(), 10,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok, err := planAutosaveTidy(c.saves, c.slots, c.loaded)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.want) == 0 {
				if ok {
					t.Fatalf("tidied: %+v", got)
				}
				return
			}
			if !ok {
				t.Fatal("no tidy")
			}
			if r := result(got, c.saves); !maps.Equal(r, c.want) {
				t.Errorf("got %v\nwant %v", r, c.want)
			}
			if got.Kept != c.kept || got.Kept+got.Removed != len(c.saves) {
				t.Errorf("kept %d removed %d", got.Kept, got.Removed)
			}
		})
	}
}

func TestPlanAutosaveTidyFallsBackToNewest(t *testing.T) {
	// Without a known loaded file K is the newest by time; with one, the
	// newest other file.
	saves := ages(numbered(1, 2, 3, 4, 5, 6)...)
	for _, loaded := range []string{"", autosaveName(6)} {
		got, _, _ := planAutosaveTidy(saves, 4, loaded)
		k := 6
		if loaded != "" {
			k = 5
		}
		newest := got.NewName(autosaveName(6))
		if want := autosaveName(((k-1)%4+4)%4 + 1); newest != want {
			t.Errorf("loaded %q: newest became %s, want %s", loaded, newest, want)
		}
	}
}

func TestPlanAutosaveTidySkips(t *testing.T) {
	cases := map[string]struct {
		saves []autosave
		slots int
	}{
		"no autosave above S": {ages(numbered(1, 2, 3)...), 5},
		"S is 0":              {ages(numbered(1, 9)...), 0},
		"no autosaves":        {nil, 3},
	}
	for name, c := range cases {
		if _, ok, err := planAutosaveTidy(c.saves, c.slots, ""); ok || err != nil {
			t.Errorf("%s: ok %v err %v", name, ok, err)
		}
	}
	if _, ok, err := planAutosaveTidy(ages(autosaveName(4), autosaveTidyName(4), autosaveName(9)), 3, ""); ok || !errors.Is(err, errAutosaveClash) {
		t.Errorf("a number held twice: ok %v err %v", ok, err)
	}
}

func tidyWorld(t *testing.T, names ...string) (*Manager, string, string) {
	t.Helper()
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	for i, name := range names {
		putSave(t, dir, name, name, tidyBase.Add(time.Duration(i)*time.Minute))
	}
	return m, id, dir
}

func dirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, SavesDir))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, SavesDir, e.Name())) //nolint:gosec // the test's own temp folder
		if err != nil {
			t.Fatal(err)
		}
		info, _ := e.Info()
		out[e.Name()] = string(b) + "@" + info.ModTime().UTC().Format(time.RFC3339Nano)
	}
	return out
}

func TestApplyAutosaveTidy(t *testing.T) {
	var names []string
	for _, n := range []int{18, 19, 20, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17} {
		names = append(names, autosaveName(n))
	}
	names = append(names, "base.zip", "_autosave21.tmp.zip")
	m, id, dir := tidyWorld(t, names...)
	before := dirState(t, dir)

	plan, ok, err := m.PlanAutosaveTidyHeld(id, 10, "")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := m.ApplyAutosaveTidyHeld(id, plan); err != nil {
		t.Fatal(err)
	}
	after := dirState(t, dir)
	want := map[string]string{"base.zip": before["base.zip"], "_autosave21.tmp.zip": before["_autosave21.tmp.zip"]}
	for i, from := range []int{17, 16, 15, 14, 13, 12, 11, 10, 9, 8} {
		// Content and modification time move with the file.
		want[autosaveName([]int{7, 6, 5, 4, 3, 2, 1, 10, 9, 8}[i])] = before[autosaveName(from)]
	}
	if !maps.Equal(after, want) {
		t.Errorf("after the tidy:\n%v\nwant\n%v", after, want)
	}

	// Run again: nothing is above S, so nothing changes.
	if _, ok, err := m.PlanAutosaveTidyHeld(id, 10, ""); ok || err != nil {
		t.Errorf("second tidy: ok %v err %v", ok, err)
	}
}

func TestAutosaveTidyResumesACutShortTidy(t *testing.T) {
	var names []string
	for _, n := range []int{18, 19, 20, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17} {
		names = append(names, autosaveName(n))
	}
	m, id, dir := tidyWorld(t, names...)
	before := dirState(t, dir)
	// The first rename happened, then Foreman stopped.
	if err := os.Rename(filepath.Join(dir, SavesDir, autosaveName(17)), filepath.Join(dir, SavesDir, autosaveTidyName(17))); err != nil {
		t.Fatal(err)
	}
	plan, ok, err := m.PlanAutosaveTidyHeld(id, 10, "")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := m.ApplyAutosaveTidyHeld(id, plan); err != nil {
		t.Fatal(err)
	}
	after := dirState(t, dir)
	if after[autosaveName(7)] != before[autosaveName(17)] || len(after) != 10 {
		t.Errorf("after: %v", after)
	}
}

func TestPlanAutosaveTidySkipsAChosenAutosave(t *testing.T) {
	m, id, _ := tidyWorld(t, numbered(1, 2, 3, 4, 5, 6)...)
	if err := m.SetLoadSave(id, autosaveName(2)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := m.PlanAutosaveTidyHeld(id, 3, ""); ok || err != nil {
		t.Errorf("tidied with an autosave chosen to load: ok %v err %v", ok, err)
	}
}

func TestAutosaveTidyCounts(t *testing.T) {
	m, id, _ := tidyWorld(t, append(numbered(1, 2, 3, 4, 5, 6, 7), "base.zip", "_autosave9.tmp.zip")...)
	cases := []struct{ slots, kept, removed int }{
		{10, 0, 0}, {7, 0, 0}, {5, 5, 2}, {3, 3, 4}, {0, 0, 0},
	}
	for _, c := range cases {
		k, r, err := m.AutosaveTidyCounts(id, c.slots)
		if err != nil || k != c.kept || r != c.removed {
			t.Errorf("slots %d: %d, %d, %v; want %d, %d", c.slots, k, r, err, c.kept, c.removed)
		}
	}
}
