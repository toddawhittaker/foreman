package game

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	saveplayersmod "github.com/toddawhittaker/foreman/internal/game/saveplayers"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// seedRig is a rig with one stopped world and a stand-in for the container
// run, which counts its calls.
type seedRig struct {
	*rig
	id    string
	mu    sync.Mutex
	calls int
	// out makes the run's result; nil gives seedOutput with the world's mods.
	out func(id string) (saveplayers.Basis, []byte, error)
	// hold, when set, keeps each run waiting until it is closed.
	hold chan struct{}
}

func newSeedRig(t *testing.T) *seedRig {
	t.Helper()
	r := &seedRig{rig: newRig(t, rigOptions{})}
	r.id = r.newSeedWorld("Players")
	r.c.seeds.read = func(ctx context.Context, id string) (saveplayers.Basis, []byte, error) {
		r.mu.Lock()
		r.calls++
		hold, out := r.hold, r.out
		r.mu.Unlock()
		if hold != nil {
			select {
			case <-hold:
			case <-ctx.Done():
				return saveplayers.Basis{}, nil, ctx.Err()
			}
		}
		if out != nil {
			return out(id)
		}
		return r.basis(id), r.seedOutput(id, nil), nil
	}
	return r
}

// newSeedWorld is a world that would start with no problem: a starting
// world gets its mod-list.json, and a read does not write one.
func (r *seedRig) newSeedWorld(name string) string {
	r.t.Helper()
	id := r.newWorld(name)
	if err := r.worlds.EnsureModList(id); err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *seedRig) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *seedRig) basis(id string) saveplayers.Basis {
	r.t.Helper()
	s, found, err := r.worlds.LoadTarget(id)
	if err != nil || !found {
		r.t.Fatalf("load target: %v %v", found, err)
	}
	return basisOfSave(s)
}

// seedOutput is the helper's output for the world's enabled mods plus extra,
// with two players.
func (r *seedRig) seedOutput(id string, extra []saveplayers.Mod) []byte {
	r.t.Helper()
	mods, err := r.worlds.Mods(id)
	if err != nil {
		r.t.Fatal(err)
	}
	type mod struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	active := []mod{{saveplayersmod.ModName, saveplayersmod.ModVersion}}
	for _, m := range mods {
		if m.Enabled {
			active = append(active, mod{m.Name, m.Version})
		}
	}
	for _, m := range extra {
		active = append(active, mod{m.Name, m.Version})
	}
	b, err := json.Marshal(map[string]any{
		"format": 1, "game_version": "2.1.24", "tick": 216000 * 3,
		"active_mods": active,
		"players": []map[string]any{
			{"name": "OrcScorpion", "online_ticks": 216000},
			{"name": "toddawhittaker", "online_ticks": 108000},
		},
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return b
}

// view asks for the view and waits for any read it starts to end.
func (r *seedRig) view() SavePlayersView {
	r.t.Helper()
	if _, err := r.c.SavePlayers(context.Background(), r.id); err != nil {
		r.t.Fatal(err)
	}
	r.waitIdle()
	v, err := r.c.SavePlayers(context.Background(), r.id)
	if err != nil {
		r.t.Fatal(err)
	}
	return v
}

func (r *seedRig) waitIdle() {
	r.t.Helper()
	eventually(r.t, func() bool { return !r.c.seedRunning(r.id) }, "the players read to end")
}

func (r *seedRig) record() (saveplayers.Record, bool) {
	r.t.Helper()
	b, err := r.worlds.ReadPlayersRecord(r.id)
	if errors.Is(err, fs.ErrNotExist) {
		return saveplayers.Record{}, false
	}
	if err != nil {
		r.t.Fatal(err)
	}
	rec, err := saveplayers.Parse(b, time.Now())
	if err != nil {
		r.t.Fatal(err)
	}
	return rec, true
}

func TestSavePlayersReadsAStoppedWorldByItself(t *testing.T) {
	r := newSeedRig(t)
	r.hold = make(chan struct{})
	v, err := r.c.SavePlayers(context.Background(), r.id)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != SavePlayersReading {
		t.Fatalf("state %q, want reading: the view must not wait for the read", v.State)
	}
	close(r.hold)
	v = r.view()
	if r.count() != 1 {
		t.Fatalf("%d reads, want 1", r.count())
	}
	if v.State != SavePlayersReady || v.Save != "base.zip" {
		t.Fatalf("view %+v, want ready for base.zip", v)
	}
	if !v.GameTimeKnown || v.GameTime != 3*time.Hour {
		t.Fatalf("game time %v (known %v), want 3h", v.GameTime, v.GameTimeKnown)
	}
	if len(v.Players) != 2 || v.Players[0].Name != "OrcScorpion" || v.Players[0].PlayTime != time.Hour ||
		!v.Players[0].BeforeForeman || !v.Players[0].LastSeen.IsZero() || v.Players[1].PlayTime != 30*time.Minute {
		t.Fatalf("players %+v", v.Players)
	}
	rec, ok := r.record()
	if !ok || rec.Seed.Result != saveplayers.ResultOK || !sameBasis(rec.Basis, r.basis(r.id)) {
		t.Fatalf("record %+v (present %v)", rec, ok)
	}
}

// Config.NoAutoPlayersRead, which test rigs set: a view starts no read, and
// "Read again" still does (SPEC 8.14).
func TestSavePlayersNoAutoReadOnlyByClick(t *testing.T) {
	r := newSeedRig(t)
	r.c.seeds.noAuto = true
	v, err := r.c.SavePlayers(context.Background(), r.id)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != SavePlayersNone || r.c.seedRunning(r.id) {
		t.Fatalf("state %q, running %v: a view started a read", v.State, r.c.seedRunning(r.id))
	}
	if err := r.c.ReadSavePlayersAgain(context.Background(), r.id, Actor{Username: "alice"}); err != nil {
		t.Fatal(err)
	}
	r.waitIdle()
	if r.count() != 1 {
		t.Fatalf("%d reads after the click, want 1", r.count())
	}
}

func TestSavePlayersMatchingRecordIsNotReadAgain(t *testing.T) {
	r := newSeedRig(t)
	r.view()
	r.view()
	if r.count() != 1 {
		t.Fatalf("%d reads, want 1", r.count())
	}
}

func TestSavePlayersReadsAgainWhenTheSaveChanges(t *testing.T) {
	r := newSeedRig(t)
	r.view()
	r.putSave(r.id, "2.1.24-no-space-age.zip", "newer.zip", time.Now())
	v := r.view()
	if r.count() != 2 || v.Save != "newer.zip" {
		t.Fatalf("%d reads, save %q; want 2 reads of newer.zip", r.count(), v.Save)
	}
}

func TestSavePlayersReadsAgainWhenInexact(t *testing.T) {
	r := newSeedRig(t)
	r.view()
	rec, _ := r.record()
	rec.Inexact = true
	b, err := rec.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.worlds.WritePlayersRecord(r.id, b); err != nil {
		t.Fatal(err)
	}
	r.hold = make(chan struct{})
	v, err := r.c.SavePlayers(context.Background(), r.id)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Inexact || v.State != SavePlayersReading || len(v.Players) != 2 {
		t.Fatalf("view %+v, want the inexact record's players while it is read again", v)
	}
	close(r.hold)
	r.waitIdle()
	if r.count() != 2 {
		t.Fatalf("%d reads, want 2", r.count())
	}
}

func TestSavePlayersCallersShareOneRead(t *testing.T) {
	r := newSeedRig(t)
	r.hold = make(chan struct{})
	for range 3 {
		if _, err := r.c.SavePlayers(context.Background(), r.id); err != nil {
			t.Fatal(err)
		}
		if err := r.c.ReadSavePlayersAgain(context.Background(), r.id, admin); err != nil {
			t.Fatal(err)
		}
	}
	close(r.hold)
	r.waitIdle()
	if r.count() != 1 {
		t.Fatalf("%d reads, want 1", r.count())
	}
}

// A read starts by itself only while no game runs on this Foreman (SPEC
// 8.16); an admin's click reads beside a running game.
func TestSavePlayersNoReadByItselfWhileAGameRuns(t *testing.T) {
	for _, which := range []string{"this world", "another world"} {
		t.Run(which, func(t *testing.T) {
			r := newSeedRig(t)
			runs := r.id
			if which == "another world" {
				runs = r.newWorld("Other")
				r.startAsActive(runs)
			} else {
				r.start(runs)
			}
			r.waitState(runs, StateRunning)
			v, err := r.c.SavePlayers(context.Background(), r.id)
			if err != nil {
				t.Fatal(err)
			}
			r.waitIdle()
			if r.count() != 0 || v.State != SavePlayersNone {
				t.Fatalf("%d reads, state %q; want no read and none", r.count(), v.State)
			}
			if err := r.c.ReadSavePlayersAgain(context.Background(), r.id, admin); err != nil {
				t.Fatal(err)
			}
			r.waitIdle()
			if r.count() != 1 {
				t.Fatalf("%d reads after the click, want 1", r.count())
			}
			if which != "this world" {
				return
			}
			// While the world runs, total play time is the live /time
			// reading, not the record's (SPEC 8.15).
			r.c.seeds.gameTime = func(string) (time.Duration, time.Time, bool) { return 5 * time.Hour, time.Now(), true }
			v, err = r.c.SavePlayers(context.Background(), r.id)
			if err != nil {
				t.Fatal(err)
			}
			if !v.GameTimeKnown || v.GameTime != 5*time.Hour {
				t.Fatalf("game time %v (known %v), want the live 5h", v.GameTime, v.GameTimeKnown)
			}
		})
	}
}

// The pre-start check runs first: a save whose mods the world lacks gives
// mods-differ and no run at all (SPEC 8.16).
func TestSavePlayersModsDifferBeforeTheRun(t *testing.T) {
	r := newSeedRig(t)
	r.c.seeds.read = nil // the real read, whose pre-start check refuses
	r.putSave(r.id, "2.1.24-space-age-testmod.zip", "modded.zip", time.Now())
	v := r.view()
	if n := len(r.rig.calls("POST /containers/create")); n != 0 {
		t.Fatalf("%d containers made, want none", n)
	}
	if v.State != SavePlayersTrackedOnly || v.Failure != saveplayers.ResultModsDiffer {
		t.Fatalf("view %+v, want tracked-only, mods-differ", v)
	}
}

func TestSavePlayersRunFailures(t *testing.T) {
	for err, want := range map[error]string{
		ErrSaveCopyImageMissing: saveplayers.ResultImageMissing,
		ErrSaveCopyOutOfMemory:  saveplayers.ResultOutOfMemory,
		ErrSaveCopyTimeout:      saveplayers.ResultTimeout,
		ErrSaveCopyModsDiffer:   saveplayers.ResultModsDiffer,
		ErrSaveCopySaveNewer:    saveplayers.ResultSaveNewer,
		ErrSaveCopyBadOutput:    saveplayers.ResultBadOutput,
	} {
		t.Run(want, func(t *testing.T) {
			r := newSeedRig(t)
			r.out = func(id string) (saveplayers.Basis, []byte, error) {
				return r.basis(id), nil, fmt.Errorf("run: %w", err)
			}
			v := r.view()
			if v.State != SavePlayersTrackedOnly || v.Failure != want {
				t.Fatalf("view %+v, want tracked-only, %s", v, want)
			}
		})
	}
}

func TestSavePlayersBadOutput(t *testing.T) {
	r := newSeedRig(t)
	r.out = func(id string) (saveplayers.Basis, []byte, error) {
		return r.basis(id), []byte(`{"format": 1, "players": "nope"}`), nil
	}
	if v := r.view(); v.Failure != saveplayers.ResultBadOutput {
		t.Fatalf("view %+v, want bad-output", v)
	}
}

// A run that did not end for a reason of the save's (a start cancelled it,
// the slot was busy) keeps nothing, so the next view tries again.
func TestSavePlayersCancelledRunKeepsNothing(t *testing.T) {
	r := newSeedRig(t)
	r.out = func(id string) (saveplayers.Basis, []byte, error) {
		return saveplayers.Basis{}, nil, context.Canceled
	}
	r.view()
	r.waitIdle()
	if _, ok := r.record(); ok {
		t.Fatal("a cancelled read wrote a record")
	}
	if r.count() != 2 {
		t.Fatalf("%d reads, want 2: the next view tries again", r.count())
	}
}

// The result is kept only if the save that loads next is still the one read
// (SPEC 8.16).
func TestSavePlayersNotKeptWhenTheSaveChangesDuringTheRun(t *testing.T) {
	r := newSeedRig(t)
	r.out = func(id string) (saveplayers.Basis, []byte, error) {
		b, out := r.basis(id), r.seedOutput(id, nil)
		r.putSave(id, "2.1.24-no-space-age.zip", "during.zip", time.Now())
		return b, out, nil
	}
	if _, err := r.c.SavePlayers(context.Background(), r.id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle()
	if _, ok := r.record(); ok {
		t.Fatal("a read of a save that is no longer the one that loads was kept")
	}
}

func TestSavePlayersNoSave(t *testing.T) {
	r := newSeedRig(t)
	if err := os.Remove(filepath.Join(r.worldDir(r.id), world.SavesDir, "base.zip")); err != nil {
		t.Fatal(err)
	}
	v, err := r.c.SavePlayers(context.Background(), r.id)
	if err != nil {
		t.Fatal(err)
	}
	r.waitIdle()
	if v.State != SavePlayersNone || r.count() != 0 {
		t.Fatalf("state %q, %d reads; want none and no read", v.State, r.count())
	}
}

func TestSavePlayersReadAgainUnknownWorld(t *testing.T) {
	r := newSeedRig(t)
	if err := r.c.ReadSavePlayersAgain(context.Background(), "nope", admin); err == nil {
		t.Fatal("a read of a world that does not exist was accepted")
	}
}

// fileFacts is what must not change about a world file during a read.
type fileFacts struct {
	sum   [32]byte
	size  int64
	mtime int64
	inode uint64
}

func worldFileFacts(t *testing.T, dir string) map[string]fileFacts {
	t.Helper()
	out := map[string]fileFacts{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == world.PlayersFile {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // a test's own temp folder
		if err != nil {
			return err
		}
		out[rel] = fileFacts{sha256.Sum256(b), info.Size(), info.ModTime().UnixNano(), info.Sys().(*syscall.Stat_t).Ino}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A read changes no world file but players.json: every other file keeps its
// SHA-256, size, modified time to the nanosecond and inode (SPEC 8.16). This
// runs the real save read against the fake game.
func TestSavePlayersReadLeavesTheWorldsFilesAlone(t *testing.T) {
	r := &seedRig{rig: newRig(t, rigOptions{})}
	r.id = r.newSeedWorld("Untouched")
	before := worldFileFacts(t, r.worldDir(r.id))
	if _, err := r.c.SavePlayers(context.Background(), r.id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle()
	rec, ok := r.record()
	if !ok || rec.Seed.Result != saveplayers.ResultOK {
		t.Fatalf("record %+v (present %v), want a good read", rec, ok)
	}
	after := worldFileFacts(t, r.worldDir(r.id))
	if len(after) != len(before) {
		t.Fatalf("%d world files after the read, %d before", len(after), len(before))
	}
	for name, f := range before {
		if after[name] != f {
			t.Errorf("%s changed: %+v, was %+v", name, after[name], f)
		}
	}
}

// A record is valid only for the save that loads: same name, size and
// modified time to the nanosecond. A record left for another save, as a read
// that ends after an upload can leave, is read again, never shown.
func TestSavePlayersRecordForAnotherSaveIsReadAgain(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(b *saveplayers.Basis)
	}{
		{"another name", func(b *saveplayers.Basis) { b.Save = "other.zip" }},
		{"another size", func(b *saveplayers.Basis) { b.Size++ }},
		{"a nanosecond off", func(b *saveplayers.Basis) { b.Modified = b.Modified.Add(time.Nanosecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newSeedRig(t)
			r.hold = make(chan struct{})
			b := r.basis(r.id)
			tc.change(&b)
			rec, err := json.Marshal(map[string]any{
				"format_version": 1, "basis": b, "as_of": time.Now(),
				"seed":          map[string]any{"at": time.Now(), "save": b.Save, "game_version": "2.1.24", "result": "ok"},
				"tracked_since": time.Now(), "inexact": false, "game_ticks": 60,
				"players": []map[string]any{{"name": "Stale", "seeded_ticks": 60, "tracked_seconds": 0, "last_seen": nil}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.worlds.WritePlayersRecord(r.id, rec); err != nil {
				t.Fatal(err)
			}
			v, err := r.c.SavePlayers(context.Background(), r.id)
			if err != nil {
				t.Fatal(err)
			}
			close(r.hold)
			r.waitIdle()
			if v.State != SavePlayersReading || len(v.Players) != 0 || v.GameTimeKnown {
				t.Fatalf("view %+v, want reading with nothing from the stale record", v)
			}
			if r.count() != 1 {
				t.Fatalf("%d reads, want 1", r.count())
			}
		})
	}
}

// A world with no mod-list.json yet (never started, a fresh import) is read
// against the list EnsureModList would write, and no file is written for it.
func TestSavePlayersWorldWithNoModList(t *testing.T) {
	r := &seedRig{rig: newRig(t, rigOptions{})}
	r.id = r.newWorld("Fresh")
	list := filepath.Join(r.worldDir(r.id), world.ModsDir, world.ModListFile)
	if _, err := os.Lstat(list); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the test world has a mod list: %v", err)
	}
	v := r.view()
	if v.State != SavePlayersReady {
		t.Fatalf("view %+v, want ready: the default list is the world's list", v)
	}
	if _, err := os.Lstat(list); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the read wrote the world's mod list: %v", err)
	}
}

// A refused record shows as none and a read by itself may replace it; a
// record from a newer Foreman is replaced only on a click, so a downgrade
// does not lose it.
func TestSavePlayersRefusedAndNewerRecords(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		autoRead bool
	}{
		{"refused", `{"format_version": 1, "players": "nope"}`, true},
		{"not JSON", `not json`, true},
		{"newer", `{"format_version": 99, "something_new": true}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newSeedRig(t)
			r.out = func(id string) (saveplayers.Basis, []byte, error) {
				return saveplayers.Basis{}, nil, context.Canceled // keep the file as it is
			}
			if err := r.worlds.WritePlayersRecord(r.id, []byte(tc.body)); err != nil {
				t.Fatal(err)
			}
			v, err := r.c.SavePlayers(context.Background(), r.id)
			if err != nil {
				t.Fatal(err)
			}
			r.waitIdle()
			if len(v.Players) != 0 || v.Failure != "" {
				t.Fatalf("view %+v, want nothing from the record", v)
			}
			if got := r.count() == 1; got != tc.autoRead {
				t.Fatalf("%d reads by itself, want a read: %v", r.count(), tc.autoRead)
			}
			if !tc.autoRead && v.State != SavePlayersNone {
				t.Fatalf("state %q, want none", v.State)
			}
			if err := r.c.ReadSavePlayersAgain(context.Background(), r.id, admin); err != nil {
				t.Fatal(err)
			}
			r.waitIdle()
			if want := map[bool]int{true: 2, false: 1}[tc.autoRead]; r.count() != want {
				t.Fatalf("%d reads after the click, want %d", r.count(), want)
			}
		})
	}
}

// sameBasis compares two bases with the time's instant, not its location: a
// record read back is in UTC.
func sameBasis(a, b saveplayers.Basis) bool {
	return a.Save == b.Save && a.Size == b.Size && a.Modified.Equal(b.Modified)
}

// A read keeps the last seen dates Foreman tracked, matching names with
// ASCII-only case folding, and never a date after the new record's as_of.
func TestKeepLastSeen(t *testing.T) {
	at := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	before, after := at.Add(-time.Hour), at.Add(time.Hour)
	old := saveplayers.Record{Players: []saveplayers.Player{
		{Name: "OrcScorpion", LastSeen: &before},
		{Name: "Late", LastSeen: &after},
		{Name: "ÉCLAIR", LastSeen: &before},
	}}
	rec := saveplayers.Record{AsOf: at, Players: []saveplayers.Player{
		{Name: "orcscorpion"}, {Name: "Late"}, {Name: "éclair"},
	}}
	keepLastSeen(&rec, old)
	if p := rec.Players[0]; p.LastSeen == nil || !p.LastSeen.Equal(before) {
		t.Errorf("orcscorpion: %v, want the old date", p.LastSeen)
	}
	if p := rec.Players[1]; p.LastSeen != nil {
		t.Errorf("Late: %v, want none (after as_of)", p.LastSeen)
	}
	if p := rec.Players[2]; p.LastSeen != nil {
		t.Errorf("éclair: %v, want none (only ASCII folds)", p.LastSeen)
	}
}
