package gamedir

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mkWorld makes worlds/<id> with a marker file in each of saves, mods and
// config that names the world, so a test can tell which world is presented.
func mkWorld(t *testing.T, dir, id string) {
	t.Helper()
	for _, name := range FixedLinks {
		writeFile(t, filepath.Join(dir, WorldsDir, id, name, "marker"), id)
	}
}

// mkOriginals makes real saves, mods and config folders as a game folder
// has before Foreman adopts it.
func mkOriginals(t *testing.T, dir string) {
	t.Helper()
	for _, name := range FixedLinks {
		writeFile(t, filepath.Join(dir, name, "marker"), "original")
		writeFile(t, filepath.Join(dir, name, "sub", "keep-"+name), "original "+name)
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, p string) {
	t.Helper()
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

// mkReady makes a folder that presents world id.
func mkReady(t *testing.T, dir, id string) {
	t.Helper()
	mkWorld(t, dir, id)
	symlink(t, WorldsDir+"/"+id, filepath.Join(dir, CurrentDir))
	for _, name := range FixedLinks {
		symlink(t, CurrentDir+"/"+name, filepath.Join(dir, name))
	}
}

func mustInspect(t *testing.T, dir string) State {
	t.Helper()
	st, err := Inspect(dir)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	return st
}

// presented reads, through the links as the game would, which world each
// of saves, mods and config shows.
func presented(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, name := range FixedLinks {
		b, err := os.ReadFile(filepath.Join(dir, name, "marker")) //nolint:gosec // a test folder
		if err != nil {
			got[name] = "(" + err.Error() + ")"
			continue
		}
		got[name] = string(b)
	}
	return got
}

// assertWhole fails unless saves, mods and config all show world want.
func assertWhole(t *testing.T, dir, want string) {
	t.Helper()
	for name, got := range presented(t, dir) {
		if got != want {
			t.Fatalf("%s shows %q, want %q (all: %v)", name, got, want, presented(t, dir))
		}
	}
}

func TestInspectStates(t *testing.T) {
	cases := []struct {
		name   string
		build  func(t *testing.T, dir string)
		kind   Kind
		id     string
		path   string
		reason string
	}{
		{name: "empty folder", build: func(*testing.T, string) {}, kind: Fresh},
		{name: "only worlds", build: func(t *testing.T, d string) { mkWorld(t, d, "a") }, kind: Fresh},
		{name: "first start stopped after one link", build: func(t *testing.T, d string) {
			mkWorld(t, d, "a")
			symlink(t, "current/saves", filepath.Join(d, "saves"))
		}, kind: Fresh},
		{name: "real folders", build: mkOriginals, kind: Unadopted},
		{name: "only a real saves folder", build: func(t *testing.T, d string) {
			writeFile(t, filepath.Join(d, "saves", "a.zip"), "x")
		}, kind: Unadopted},
		{name: "ready", build: func(t *testing.T, d string) { mkReady(t, d, "my-world-2") }, kind: Ready, id: "my-world-2"},
		{name: "leftover current.new is ignored", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			mkWorld(t, d, "b")
			symlink(t, "worlds/b", filepath.Join(d, "current.new"))
		}, kind: Ready, id: "a"},
		{name: "worlds is a link", build: func(t *testing.T, d string) {
			other := t.TempDir()
			mkWorld(t, other, "a")
			symlink(t, filepath.Join(other, WorldsDir), filepath.Join(d, WorldsDir))
		}, kind: Broken, path: "worlds", reason: "link"},
		{name: "worlds is a file", build: func(t *testing.T, d string) {
			writeFile(t, filepath.Join(d, WorldsDir), "x")
		}, kind: Broken, path: "worlds"},
		{name: "current is absolute", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			_ = os.Remove(filepath.Join(d, CurrentDir))
			symlink(t, filepath.Join(d, "worlds", "a"), filepath.Join(d, CurrentDir))
		}, kind: Broken, path: "current"},
		{name: "current has dot slash", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			_ = os.Remove(filepath.Join(d, CurrentDir))
			symlink(t, "./worlds/a", filepath.Join(d, CurrentDir))
		}, kind: Broken, path: "current"},
		{name: "current climbs out", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			_ = os.Remove(filepath.Join(d, CurrentDir))
			symlink(t, "worlds/../../etc", filepath.Join(d, CurrentDir))
		}, kind: Broken, path: "current"},
		{name: "current names an invalid id", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			mkWorld(t, d, "Bad_ID")
			_ = os.Remove(filepath.Join(d, CurrentDir))
			symlink(t, "worlds/Bad_ID", filepath.Join(d, CurrentDir))
		}, kind: Broken, path: "current"},
		{name: "current is a real folder", build: func(t *testing.T, d string) {
			writeFile(t, filepath.Join(d, CurrentDir, "saves", "a.zip"), "x")
		}, kind: Broken, path: "current"},
		{name: "current names a missing world", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			_ = os.RemoveAll(filepath.Join(d, WorldsDir, "a"))
		}, kind: Broken, path: "worlds/a"},
		{name: "world folder is a link", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			mkWorld(t, d, "b")
			_ = os.RemoveAll(filepath.Join(d, WorldsDir, "a"))
			symlink(t, "b", filepath.Join(d, WorldsDir, "a"))
		}, kind: Broken, path: "worlds/a"},
		{name: "fixed link points elsewhere", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			_ = os.Remove(filepath.Join(d, "mods"))
			symlink(t, "worlds/a/mods", filepath.Join(d, "mods"))
		}, kind: Broken, path: "mods"},
		{name: "fixed link missing when ready", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			_ = os.Remove(filepath.Join(d, "config"))
		}, kind: Broken, path: "config"},
		{name: "fixed name is a real folder when ready", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			_ = os.Remove(filepath.Join(d, "saves"))
			writeFile(t, filepath.Join(d, "saves", "a.zip"), "x")
		}, kind: Broken, path: "saves"},
		{name: "fixed name is a file", build: func(t *testing.T, d string) {
			writeFile(t, filepath.Join(d, "mods"), "x")
		}, kind: Broken, path: "mods"},
		{name: "mix of links and real folders", build: func(t *testing.T, d string) {
			mkOriginals(t, d)
			_ = os.RemoveAll(filepath.Join(d, "saves"))
			symlink(t, "current/saves", filepath.Join(d, "saves"))
		}, kind: Broken, path: "saves"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.build(t, dir)
			st := mustInspect(t, dir)
			if st.Kind != c.kind || st.ActiveID != c.id {
				t.Fatalf("got %+v, want kind %s id %q", st, c.kind, c.id)
			}
			if c.path != "" && st.Path != c.path {
				t.Fatalf("path %q, want %q (%s)", st.Path, c.path, st.Reason)
			}
			if c.kind == Broken && st.Reason == "" {
				t.Fatal("broken with no reason")
			}
			if c.reason != "" && !strings.Contains(st.Reason, c.reason) {
				t.Fatalf("reason %q does not mention %q", st.Reason, c.reason)
			}
		})
	}
}

func TestPresentFromFresh(t *testing.T) {
	dir := t.TempDir()
	mkWorld(t, dir, "a")
	if err := Present(dir, "a"); err != nil {
		t.Fatal(err)
	}
	if st := mustInspect(t, dir); st.Kind != Ready || st.ActiveID != "a" {
		t.Fatalf("got %+v", st)
	}
	assertWhole(t, dir, "a")
	// The links are relative, so they resolve the same inside the game
	// container where the folder is mounted elsewhere.
	if got, _ := os.Readlink(filepath.Join(dir, CurrentDir)); got != "worlds/a" {
		t.Fatalf("current links to %q", got)
	}
}

func TestPresentSwitches(t *testing.T) {
	dir := t.TempDir()
	mkReady(t, dir, "a")
	mkWorld(t, dir, "b")
	if err := Present(dir, "b"); err != nil {
		t.Fatal(err)
	}
	if st := mustInspect(t, dir); st.Kind != Ready || st.ActiveID != "b" {
		t.Fatalf("got %+v", st)
	}
	assertWhole(t, dir, "b")
	if _, err := os.Lstat(filepath.Join(dir, "current.new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("current.new left behind: %v", err)
	}
}

func TestPresentRefuses(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, dir string)
		id    string
		want  error
	}{
		{name: "unadopted", build: func(t *testing.T, d string) { mkOriginals(t, d); mkWorld(t, d, "a") }, id: "a", want: ErrWrongState},
		{name: "broken", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			_ = os.Remove(filepath.Join(d, "mods"))
		}, id: "a", want: ErrWrongState},
		{name: "missing world", build: func(t *testing.T, d string) { mkReady(t, d, "a") }, id: "b", want: ErrNoWorld},
		{name: "no worlds folder", build: func(*testing.T, string) {}, id: "a", want: ErrNoWorld},
		{name: "path in id", build: func(t *testing.T, d string) { mkReady(t, d, "a") }, id: "../a", want: ErrNoWorld},
		{name: "world is a link", build: func(t *testing.T, d string) {
			mkReady(t, d, "a")
			symlink(t, "a", filepath.Join(d, WorldsDir, "b"))
		}, id: "b", want: ErrNoWorld},
		{name: "worlds is a link", build: func(t *testing.T, d string) {
			other := t.TempDir()
			mkWorld(t, other, "a")
			symlink(t, filepath.Join(other, WorldsDir), filepath.Join(d, WorldsDir))
		}, id: "a", want: ErrWrongState},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.build(t, dir)
			before := mustInspect(t, dir)
			if err := Present(dir, c.id); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
			if after := mustInspect(t, dir); after != before {
				t.Fatalf("refusal changed the folder: %+v then %+v", before, after)
			}
		})
	}
}

var cutTime = time.Date(2026, 11, 4, 12, 30, 5, 0, time.FixedZone("x", 3600))

func TestCutOver(t *testing.T) {
	dir := t.TempDir()
	mkOriginals(t, dir)
	mkWorld(t, dir, "base")
	pre, err := CutOver(dir, "base", cutTime)
	if err != nil {
		t.Fatal(err)
	}
	if pre != "pre-foreman-20261104T113005Z" {
		t.Fatalf("pre-foreman folder %q, want UTC time", pre)
	}
	if st := mustInspect(t, dir); st.Kind != Ready || st.ActiveID != "base" {
		t.Fatalf("got %+v", st)
	}
	assertWhole(t, dir, "base")
	assertOriginalsKept(t, dir)
}

func TestCutOverRefuses(t *testing.T) {
	for name, build := range map[string]func(t *testing.T, d string){
		"fresh": func(t *testing.T, d string) { mkWorld(t, d, "base") },
		"ready": func(t *testing.T, d string) { mkReady(t, d, "base") },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			build(t, dir)
			if _, err := CutOver(dir, "base", cutTime); !errors.Is(err, ErrWrongState) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("missing world", func(t *testing.T) {
		dir := t.TempDir()
		mkOriginals(t, dir)
		if _, err := CutOver(dir, "base", cutTime); !errors.Is(err, ErrNoWorld) {
			t.Fatalf("got %v", err)
		}
		if st := mustInspect(t, dir); st.Kind != Unadopted {
			t.Fatalf("got %+v", st)
		}
	})
}

func TestRecoverRemovesLeftovers(t *testing.T) {
	dir := t.TempDir()
	mkReady(t, dir, "a")
	mkWorld(t, dir, "b")
	symlink(t, "worlds/b", filepath.Join(dir, "current.new"))
	symlink(t, "current/mods", filepath.Join(dir, "mods.new"))
	if err := Recover(dir); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"current.new", "mods.new"} {
		if _, err := os.Lstat(filepath.Join(dir, n)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still there: %v", n, err)
		}
	}
	if st := mustInspect(t, dir); st.ActiveID != "a" {
		t.Fatalf("got %+v", st)
	}
}

func TestRecoverNeverRemovesARealFolder(t *testing.T) {
	dir := t.TempDir()
	mkReady(t, dir, "a")
	writeFile(t, filepath.Join(dir, "saves.new", "precious.zip"), "x")
	if err := Recover(dir); !errors.Is(err, ErrWrongState) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "saves.new", "precious.zip")); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverLeavesUnadoptedAlone(t *testing.T) {
	dir := t.TempDir()
	mkOriginals(t, dir)
	if err := Recover(dir); err != nil {
		t.Fatal(err)
	}
	if st := mustInspect(t, dir); st.Kind != Unadopted {
		t.Fatalf("got %+v", st)
	}
	assertWhole(t, dir, "original")
}

// assertOriginalsKept fails unless every original file is somewhere in the
// folder: still in place or inside a pre-foreman folder.
func assertOriginalsKept(t *testing.T, dir string) {
	t.Helper()
	pres, _ := filepath.Glob(filepath.Join(dir, PreForemanPrefix+"*"))
	for _, name := range FixedLinks {
		rel := filepath.Join(name, "sub", "keep-"+name)
		places := []string{filepath.Join(dir, rel)}
		for _, p := range pres {
			places = append(places, filepath.Join(p, rel))
		}
		found := 0
		for _, p := range places {
			// Lstat on the parent folders: an original reached only through
			// a link into a world would not count.
			if fi, err := os.Lstat(filepath.Dir(filepath.Dir(p))); err != nil || fi.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if b, err := os.ReadFile(p); err == nil && string(b) == "original "+name { //nolint:gosec // a test folder
				found++
			}
		}
		if found != 1 {
			t.Fatalf("%s found in %d places, want 1", rel, found)
		}
	}
}

// crashCase is one change to the game folder, run from a fresh copy of
// its starting layout.
type crashCase struct {
	name  string
	build func(t *testing.T, dir string)
	// run makes the change; retry is true for the second attempt, which
	// a cut-over makes at a later time.
	run func(dir string, retry bool) error
	// from is what the folder presented before; to is what it presents
	// after the change. After a crash and Recover it must be one of them.
	from, to  string
	originals bool
}

var crashCases = []crashCase{
	{
		name:  "cut-over",
		build: func(t *testing.T, d string) { mkOriginals(t, d); mkWorld(t, d, "base") },
		run: func(d string, retry bool) error {
			at := cutTime
			if retry {
				at = at.Add(time.Hour)
			}
			_, err := CutOver(d, "base", at)
			return err
		},
		from: "original", to: "base", originals: true,
	},
	{
		name:  "first start",
		build: func(t *testing.T, d string) { mkWorld(t, d, "a") },
		run:   func(d string, _ bool) error { return Present(d, "a") },
		from:  "", to: "a",
	},
	{
		name:  "switch",
		build: func(t *testing.T, d string) { mkReady(t, d, "a"); mkWorld(t, d, "b") },
		run:   func(d string, _ bool) error { return Present(d, "b") },
		from:  "a", to: "b",
	},
}

var errCrash = errors.New("simulated crash")

// crashAfter makes the next change stop after n steps on disk. It returns
// a function that reports how many steps ran.
func crashAfter(t *testing.T, n int) func() int {
	t.Helper()
	count := 0
	afterStep = func() error {
		count++
		if n > 0 && count >= n {
			return errCrash
		}
		return nil
	}
	t.Cleanup(func() { afterStep = nil })
	return func() int { return count }
}

// checkSettled fails unless the folder presents exactly from or to, whole,
// and has lost no original file.
func checkSettled(t *testing.T, dir string, c crashCase) {
	t.Helper()
	st := mustInspect(t, dir)
	switch {
	case st.Kind == Ready && (st.ActiveID == c.to || st.ActiveID == c.from):
		assertWhole(t, dir, st.ActiveID)
	case c.from == "original" && st.Kind == Unadopted:
		assertWhole(t, dir, "original")
	case c.from == "" && st.Kind == Fresh:
	default:
		t.Fatalf("after recovery: %+v, presents %v", st, presented(t, dir))
	}
	for _, n := range []string{"current.new", "saves.new", "mods.new", "config.new"} {
		if _, err := os.Lstat(filepath.Join(dir, n)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s left behind after recovery", n)
		}
	}
	if c.originals {
		assertOriginalsKept(t, dir)
	}
}

// TestCrashAtEveryStep stops each change after every single step on disk,
// then runs Recover, and checks the folder presents one whole world, never
// a mix, with no file lost (SPEC 4.8). Recover itself is also stopped at
// every step and run again. A retried change must then succeed.
func TestCrashAtEveryStep(t *testing.T) {
	for _, c := range crashCases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.build(t, dir)
			steps := crashAfter(t, 0)
			if err := c.run(dir, false); err != nil {
				t.Fatal(err)
			}
			total := steps()
			if total < 2 {
				t.Fatalf("only %d steps counted", total)
			}
			for k := 1; k <= total; k++ {
				for j := 0; j <= total; j++ {
					dir := t.TempDir()
					c.build(t, dir)
					crashAfter(t, k)
					if err := c.run(dir, false); k < total && !errors.Is(err, errCrash) {
						t.Fatalf("crash at %d: got %v", k, err)
					}
					// j > 0 stops Recover too, after j steps.
					crashAfter(t, j)
					if err := Recover(dir); err != nil && !errors.Is(err, errCrash) {
						t.Fatalf("crash at %d, recover stop %d: %v", k, j, err)
					}
					afterStep = nil
					if err := Recover(dir); err != nil {
						t.Fatalf("crash at %d, recover stop %d, second recover: %v", k, j, err)
					}
					checkSettled(t, dir, c)
					if err := c.run(dir, true); err != nil && (!c.originals || !errors.Is(err, ErrWrongState)) {
						t.Fatalf("crash at %d, recover stop %d, retry: %v", k, j, err)
					}
					if st := mustInspect(t, dir); st.Kind != Ready || st.ActiveID != c.to {
						t.Fatalf("crash at %d, recover stop %d, after retry: %+v", k, j, st)
					}
					assertWhole(t, dir, c.to)
					if c.originals {
						assertOriginalsKept(t, dir)
					}
				}
			}
		})
	}
}

func TestOperationsStayInsideTheFolder(t *testing.T) {
	// A fixed link that points outside is reported, never followed or
	// replaced.
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "keep"), "x")
	dir := t.TempDir()
	mkReady(t, dir, "a")
	_ = os.Remove(filepath.Join(dir, "saves"))
	symlink(t, outside, filepath.Join(dir, "saves"))
	if st := mustInspect(t, dir); st.Kind != Broken || st.Path != "saves" {
		t.Fatalf("got %+v", st)
	}
	if err := Present(dir, "a"); !errors.Is(err, ErrWrongState) {
		t.Fatalf("got %v", err)
	}
	if err := Recover(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(filepath.Join(dir, "saves")); got != outside {
		t.Fatalf("saves link changed to %q", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal(err)
	}
}
