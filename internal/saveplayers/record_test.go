package saveplayers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var (
	t0    = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	basis = Basis{Save: "_autosave3.zip", Size: 164000000, Modified: time.Date(2026, 11, 21, 10, 0, 0, 123456789, time.UTC)}
)

func seeded(t *testing.T) Record {
	t.Helper()
	s, err := ParseSeed([]byte(goodSeed))
	if err != nil {
		t.Fatal(err)
	}
	return FromSeed(s, basis, t0)
}

func player(t *testing.T, r Record, name string) Player {
	t.Helper()
	for _, p := range r.Players {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no player %q in %+v", name, r.Players)
	return Player{}
}

func TestFromSeed(t *testing.T) {
	r := seeded(t)
	if r.FormatVersion != FormatVersion || !r.Basis.Same(basis) || !r.AsOf.Equal(t0) || !r.TrackedSince.Equal(t0) {
		t.Fatalf("header: %+v", r)
	}
	if r.Seed.Result != ResultOK || r.Seed.Save != "_autosave3.zip" || r.Seed.GameVersion != "2.0.77" || !r.Seed.At.Equal(t0) {
		t.Fatalf("seed: %+v", r.Seed)
	}
	if r.Inexact || r.GameTicks != 8352001 {
		t.Fatalf("inexact %v, ticks %d", r.Inexact, r.GameTicks)
	}
	o := player(t, r, "OrcScorpion")
	if o.SeededTicks != 28484153 || o.TrackedSeconds != 0 || o.LastSeen != nil {
		t.Fatalf("%+v", o)
	}
	if !r.Seeded() {
		t.Fatal("a good read is seeded")
	}
}

func TestFailedHasNoSeededTotals(t *testing.T) {
	for _, res := range []string{ResultImageMissing, ResultOutOfMemory, ResultTimeout, ResultModsDiffer, ResultSaveNewer, ResultBadOutput} {
		r := Failed(res, basis, t0)
		if r.Seed.Result != res || r.Seeded() {
			t.Fatalf("%s: %+v", res, r.Seed)
		}
		if len(r.Players) != 0 || r.GameTicks != 0 || !r.TrackedSince.Equal(t0) || !r.AsOf.Equal(t0) || r.Inexact {
			t.Fatalf("%s: %+v", res, r)
		}
		if _, err := r.Marshal(); err != nil {
			t.Fatalf("%s: a failed record must be writable: %v", res, err)
		}
	}
}

// A result that is not a failure id (ok, or nonsense) never makes a record
// that claims a good read with no figures.
func TestFailedNormalisesUnknownResult(t *testing.T) {
	for _, res := range []string{ResultOK, "", "who knows"} {
		if r := Failed(res, basis, t0); r.Seed.Result != ResultBadOutput || r.Seeded() {
			t.Fatalf("%q gave %q", res, r.Seed.Result)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	r := seeded(t)
	r.AddSession("OrcScorpion", t0.Add(time.Minute), t0.Add(time.Hour))
	r.Inexact = true
	b, err := r.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := got.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, b2) {
		t.Fatalf("not stable:\n%s\n%s", b, b2)
	}
	if !got.Basis.Same(r.Basis) || got.Basis.Modified.Nanosecond() != 123456789 {
		t.Fatalf("basis lost its nanoseconds: %+v", got.Basis)
	}
	if !got.Inexact || got.GameTicks != r.GameTicks || len(got.Players) != 2 {
		t.Fatalf("%+v", got)
	}
}

// The format is the one in the design: snake_case keys, "players" an array
// even when empty, last_seen null.
func TestMarshalShape(t *testing.T) {
	b, err := seeded(t).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"format_version", "basis", "as_of", "seed", "tracked_since", "inexact", "game_ticks", "players"} {
		if _, ok := m[k]; !ok {
			t.Errorf("no %q in %s", k, b)
		}
	}
	if !strings.Contains(string(b), `"last_seen": null`) {
		t.Errorf("last_seen should be null: %s", b)
	}
	empty := Failed(ResultTimeout, basis, t0)
	empty.Players = nil
	b, err = empty.Marshal()
	if err != nil || !strings.Contains(string(b), `"players": []`) {
		t.Fatalf("%v %s", err, b)
	}
}

func TestMarshalRefusesWhatParseRefuses(t *testing.T) {
	r := seeded(t)
	r.Players = append(r.Players, Player{Name: "has space"})
	if _, err := r.Marshal(); err == nil {
		t.Fatal("wrote a record Parse would refuse")
	}
	if _, err := (Record{}).Marshal(); err == nil {
		t.Fatal("wrote the zero record")
	}
}

func goodRecordJSON(t *testing.T) map[string]any {
	t.Helper()
	b, err := seeded(t).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseRefuses(t *testing.T) {
	set := func(f func(m map[string]any)) string {
		m := goodRecordJSON(t)
		f(m)
		b, _ := json.Marshal(m)
		return string(b)
	}
	pl := func(m map[string]any) map[string]any { return m["players"].([]any)[0].(map[string]any) }
	cases := map[string]string{
		"empty":            ``,
		"not json":         `{`,
		"array":            `[]`,
		"trailing":         set(func(m map[string]any) {}) + `{}`,
		"unknown field":    set(func(m map[string]any) { m["extra"] = 1 }),
		"unknown nested":   set(func(m map[string]any) { m["basis"].(map[string]any)["x"] = 1 }),
		"unknown player":   set(func(m map[string]any) { pl(m)["x"] = 1 }),
		"zero format":      set(func(m map[string]any) { m["format_version"] = 0 }),
		"no basis save":    set(func(m map[string]any) { m["basis"].(map[string]any)["save"] = "" }),
		"save with slash":  set(func(m map[string]any) { m["basis"].(map[string]any)["save"] = "../x.zip" }),
		"save with nul":    set(func(m map[string]any) { m["basis"].(map[string]any)["save"] = "a\u0000.zip" }),
		"save too long":    set(func(m map[string]any) { m["basis"].(map[string]any)["save"] = strings.Repeat("a", 300) }),
		"negative size":    set(func(m map[string]any) { m["basis"].(map[string]any)["size"] = -1 }),
		"bad time":         set(func(m map[string]any) { m["as_of"] = "yesterday" }),
		"zero as_of":       set(func(m map[string]any) { m["as_of"] = "0001-01-01T00:00:00Z" }),
		"result unknown":   set(func(m map[string]any) { m["seed"].(map[string]any)["result"] = "great" }),
		"result empty":     set(func(m map[string]any) { m["seed"].(map[string]any)["result"] = "" }),
		"version odd":      set(func(m map[string]any) { m["seed"].(map[string]any)["game_version"] = "<b>" }),
		"negative ticks":   set(func(m map[string]any) { m["game_ticks"] = -1 }),
		"ticks over 2^53":  set(func(m map[string]any) { m["game_ticks"] = float64(MaxWhole) * 2 }),
		"seeded negative":  set(func(m map[string]any) { pl(m)["seeded_ticks"] = -1 }),
		"seeded over 2^53": set(func(m map[string]any) { pl(m)["seeded_ticks"] = float64(MaxWhole) * 2 }),
		"tracked negative": set(func(m map[string]any) { pl(m)["tracked_seconds"] = -1 }),
		"tracked fraction": set(func(m map[string]any) { pl(m)["tracked_seconds"] = 1.5 }),
		"bad name":         set(func(m map[string]any) { pl(m)["name"] = "a b" }),
		"empty name":       set(func(m map[string]any) { pl(m)["name"] = "" }),
		"duplicate name":   set(func(m map[string]any) { m["players"] = append(m["players"].([]any), pl(m)) }),
		"duplicate case": set(func(m map[string]any) {
			c := map[string]any{}
			for k, v := range pl(m) {
				c[k] = v
			}
			c["name"] = "orcscorpion"
			m["players"] = append(m["players"].([]any), c)
		}),
		"bad last_seen":  set(func(m map[string]any) { pl(m)["last_seen"] = "soon" }),
		"zero last_seen": set(func(m map[string]any) { pl(m)["last_seen"] = "0001-01-01T00:00:00Z" }),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(in)); err == nil {
				t.Fatal("accepted")
			} else if !errors.Is(err, ErrBadRecord) {
				t.Fatalf("error does not wrap ErrBadRecord: %v", err)
			}
		})
	}
}

func TestParseNewerFormatIsDistinct(t *testing.T) {
	m := goodRecordJSON(t)
	m["format_version"] = FormatVersion + 1
	m["something_new"] = true
	b, _ := json.Marshal(m)
	_, err := Parse(b)
	if !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("want ErrNewerFormat, got %v", err)
	}
}

func TestParseTooBig(t *testing.T) {
	b, err := seeded(t).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(append(b, bytes.Repeat([]byte(" "), MaxRecordBytes)...)); !errors.Is(err, ErrBadRecord) {
		t.Fatalf("%v", err)
	}
}

func TestParseTooManyPlayers(t *testing.T) {
	r := seeded(t)
	for len(r.Players) <= MaxPlayers {
		r.Players = append(r.Players, Player{Name: fmt.Sprintf("p%d", len(r.Players))})
	}
	if _, err := r.Marshal(); err == nil {
		t.Fatal("wrote too many players")
	}
}

func TestParseKeepsNullLastSeen(t *testing.T) {
	b, _ := seeded(t).Marshal()
	r, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if player(t, r, "OrcScorpion").LastSeen != nil {
		t.Fatal("null last_seen became a date")
	}
}

func TestPlayedSeconds(t *testing.T) {
	p := Player{SeededTicks: 60*3600 + 59, TrackedSeconds: 125}
	if got := p.PlayedSeconds(); got != 3600+125 {
		t.Fatalf("%d", got)
	}
	if got := (Record{GameTicks: 60*90 + 30}).GameSeconds(); got != 90 {
		t.Fatalf("%d", got)
	}
}

func TestBasisSame(t *testing.T) {
	b := basis
	b.Modified = b.Modified.In(time.FixedZone("x", 3600))
	if !basis.Same(b) {
		t.Fatal("the same instant in another zone is the same save")
	}
	for _, o := range []Basis{
		{Save: "other.zip", Size: basis.Size, Modified: basis.Modified},
		{Save: basis.Save, Size: basis.Size + 1, Modified: basis.Modified},
		{Save: basis.Save, Size: basis.Size, Modified: basis.Modified.Add(time.Nanosecond)},
	} {
		if basis.Same(o) {
			t.Fatalf("%+v is not the same save", o)
		}
	}
}

// (a) only the part after AsOf counts.
func TestAddSessionCountsOnlyAfterAsOf(t *testing.T) {
	r := seeded(t)
	r.AddSession("OrcScorpion", t0.Add(-10*time.Minute), t0.Add(5*time.Minute))
	if got := player(t, r, "OrcScorpion").TrackedSeconds; got != 300 {
		t.Fatalf("tracked %d, want 300", got)
	}
	r.AddSession("OrcScorpion", t0.Add(time.Hour), t0.Add(2*time.Hour))
	if got := player(t, r, "OrcScorpion").TrackedSeconds; got != 300+3600 {
		t.Fatalf("tracked %d", got)
	}
}

// (a) a session that ends at or before AsOf adds no time.
func TestAddSessionAtOrBeforeAsOfAddsNoTime(t *testing.T) {
	r := seeded(t)
	r.AddSession("OrcScorpion", t0.Add(-time.Hour), t0)
	r.AddSession("OrcScorpion", t0.Add(-2*time.Hour), t0.Add(-time.Hour))
	if got := player(t, r, "OrcScorpion").TrackedSeconds; got != 0 {
		t.Fatalf("tracked %d", got)
	}
}

// AddSession never moves AsOf: the caller does, once every open session is
// folded.
func TestAddSessionDoesNotMoveAsOf(t *testing.T) {
	r := seeded(t)
	r.AddSession("OrcScorpion", t0, t0.Add(time.Hour))
	if !r.AsOf.Equal(t0) {
		t.Fatalf("as_of moved to %v", r.AsOf)
	}
}

// Two players on together, folded at one moment from the same AsOf, each
// count their whole time. The caller sets AsOf afterwards.
func TestAddSessionOverlapFoldedAtOneMoment(t *testing.T) {
	r := seeded(t)
	now := t0.Add(40 * time.Minute)
	r.AddSession("OrcScorpion", t0, t0.Add(30*time.Minute))
	r.AddSession("toddawhittaker", t0.Add(10*time.Minute), now)
	r.AsOf = now
	if got := player(t, r, "OrcScorpion").TrackedSeconds; got != 1800 {
		t.Fatalf("first %d", got)
	}
	if got := player(t, r, "toddawhittaker").TrackedSeconds; got != 1800 {
		t.Fatalf("second %d, want 1800", got)
	}
	// Replaying the same lines after the caller moved AsOf adds nothing.
	r.AddSession("OrcScorpion", t0, t0.Add(30*time.Minute))
	r.AddSession("toddawhittaker", t0.Add(10*time.Minute), now)
	if player(t, r, "OrcScorpion").TrackedSeconds != 1800 || player(t, r, "toddawhittaker").TrackedSeconds != 1800 {
		t.Fatalf("a replay counted again: %+v", r.Players)
	}
}

// (b) last seen is the session's end, whether or not time was counted.
func TestAddSessionSetsLastSeen(t *testing.T) {
	r := seeded(t)
	r.AddSession("OrcScorpion", t0, t0.Add(time.Hour))
	if got := player(t, r, "OrcScorpion").LastSeen; got == nil || !got.Equal(t0.Add(time.Hour)) {
		t.Fatalf("%v", got)
	}
	r.AsOf = t0.Add(3 * time.Hour)
	r.AddSession("toddawhittaker", t0, t0.Add(2*time.Hour)) // wholly before AsOf
	if got := player(t, r, "toddawhittaker"); got.LastSeen == nil || !got.LastSeen.Equal(t0.Add(2*time.Hour)) || got.TrackedSeconds != 0 {
		t.Fatalf("%+v", got)
	}
}

// (c) a missing player is added, even for a session of no length.
func TestAddSessionAddsMissingPlayerEvenAtZeroLength(t *testing.T) {
	r := seeded(t)
	at := t0.Add(time.Minute)
	r.AddSession("Fresh", at, at)
	p := player(t, r, "Fresh")
	if p.TrackedSeconds != 0 || p.SeededTicks != 0 || p.LastSeen == nil || !p.LastSeen.Equal(at) {
		t.Fatalf("%+v", p)
	}
	if _, err := r.Marshal(); err != nil {
		t.Fatal(err)
	}
}

func TestAddSessionIgnoresNonsense(t *testing.T) {
	r := seeded(t)
	before, _ := r.Marshal()
	r.AddSession("a b", t0, t0.Add(time.Hour))                              // not a name
	r.AddSession("", t0, t0.Add(time.Hour))                                 // no name
	r.AddSession("OrcScorpion", t0.Add(time.Hour), t0)                      // backwards
	r.AddSession("Fresh", t0.Add(time.Hour), t0.Add(time.Hour-time.Second)) // backwards
	after, _ := r.Marshal()
	if !bytes.Equal(before, after) {
		t.Fatalf("changed:\n%s\n%s", before, after)
	}
}

func TestAddSessionNewPlayerAndCaseInsensitive(t *testing.T) {
	r := seeded(t)
	r.AddSession("NewPerson", t0, t0.Add(time.Hour))
	if got := player(t, r, "NewPerson"); got.TrackedSeconds != 3600 || got.SeededTicks != 0 {
		t.Fatalf("%+v", got)
	}
	// The game compares names without regard to case: one player.
	r.AddSession("orcscorpion", t0, t0.Add(time.Hour))
	if len(r.Players) != 3 {
		t.Fatalf("a case difference made a new player: %+v", r.Players)
	}
	if got := player(t, r, "OrcScorpion"); got.TrackedSeconds != 3600 {
		t.Fatalf("%+v", got)
	}
}

func TestAddSessionRoundsToSeconds(t *testing.T) {
	r := seeded(t)
	r.AddSession("OrcScorpion", t0, t0.Add(10*time.Second+400*time.Millisecond))
	r.AddSession("OrcScorpion", t0.Add(11*time.Second), t0.Add(20*time.Second+600*time.Millisecond))
	if got := player(t, r, "OrcScorpion").TrackedSeconds; got != 10+10 {
		t.Fatalf("tracked %d", got)
	}
}

func TestAddSessionKeepsLatestLastSeen(t *testing.T) {
	r := seeded(t)
	r.AddSession("OrcScorpion", t0, t0.Add(time.Hour))
	r.AddSession("OrcScorpion", t0.Add(2*time.Hour), t0.Add(3*time.Hour))
	got := player(t, r, "OrcScorpion").LastSeen
	if got == nil || !got.Equal(t0.Add(3*time.Hour)) {
		t.Fatalf("%v", got)
	}
}

func TestAddSessionCapsPlayers(t *testing.T) {
	r := seeded(t)
	r.Players = r.Players[:0]
	for i := 0; i < MaxPlayers; i++ {
		r.Players = append(r.Players, Player{Name: fmt.Sprintf("p%d", i)})
	}
	r.AddSession("OneTooMany", t0, t0.Add(time.Hour))
	if len(r.Players) != MaxPlayers {
		t.Fatalf("%d players", len(r.Players))
	}
}

func TestSetGameTicks(t *testing.T) {
	r := seeded(t)
	r.SetGameTicks(9_000_000, t0.Add(time.Hour))
	if r.GameTicks != 9_000_000 {
		t.Fatalf("%d", r.GameTicks)
	}
	r.SetGameTicks(-5, t0)
	r.SetGameTicks(MaxWhole+1, t0)
	if r.GameTicks != 9_000_000 {
		t.Fatalf("a bad value was kept: %d", r.GameTicks)
	}
	if !r.AsOf.Equal(t0) {
		t.Fatal("game time must not move the sessions' as_of")
	}
}

func TestRebaseKeepsWhatWasTracked(t *testing.T) {
	r := seeded(t)
	r.AddSession("OrcScorpion", t0, t0.Add(time.Hour))
	r.Inexact = true
	before := r
	before.Players = append([]Player(nil), r.Players...)
	nb := Basis{Save: "_autosave4.zip", Size: 170000000, Modified: t0.Add(2 * time.Hour)}
	r.Rebase(nb, t0.Add(2*time.Hour))
	if !r.Basis.Same(nb) {
		t.Fatalf("basis %+v", r.Basis)
	}
	if !reflect.DeepEqual(r.Players, before.Players) || r.Seed != before.Seed || !r.AsOf.Equal(before.AsOf) ||
		!r.TrackedSince.Equal(before.TrackedSince) || r.GameTicks != before.GameTicks || r.Inexact != before.Inexact {
		t.Fatalf("rebase changed more than the basis:\n%+v\n%+v", before, r)
	}
}
