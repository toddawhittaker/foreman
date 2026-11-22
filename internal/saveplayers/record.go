package saveplayers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// FromSeed makes the record a good read gives: every player's time from the
// save, tracked from at. at is the moment the record's totals are true for.
func FromSeed(s Seed, b Basis, at time.Time) Record {
	r := newRecord(b, at)
	r.Seed = SeedInfo{At: at.UTC(), Save: b.Save, GameVersion: s.GameVersion, Result: ResultOK}
	r.GameTicks = s.Tick
	for _, p := range s.Players {
		r.Players = append(r.Players, Player{Name: p.Name, SeededTicks: p.OnlineTicks})
	}
	return r
}

// Failed makes the record a failed read gives: tracked from at, with no
// seeded totals. result is one of the failure ids; anything else (including
// ResultOK) is stored as ResultBadOutput, so a record never claims a read
// that gave nothing.
func Failed(result string, b Basis, at time.Time) Record {
	if !failureResult(result) {
		result = ResultBadOutput
	}
	r := newRecord(b, at)
	r.Seed = SeedInfo{At: at.UTC(), Save: b.Save, Result: result}
	return r
}

func newRecord(b Basis, at time.Time) Record {
	b.Modified = b.Modified.UTC()
	return Record{
		FormatVersion: FormatVersion,
		Basis:         b,
		AsOf:          at.UTC(),
		TrackedSince:  at.UTC(),
		Players:       []Player{},
	}
}

func failureResult(s string) bool {
	switch s {
	case ResultImageMissing, ResultOutOfMemory, ResultTimeout, ResultModsDiffer, ResultSaveNewer, ResultBadOutput:
		return true
	}
	return false
}

// Parse reads players.json bytes. The file sits in a folder the game can
// write, so it is read as untrusted: at most MaxRecordBytes, no field it does
// not know, every value checked, and dates that make sense against now (the
// caller's clock): as_of no later than now plus MaxClockSkew, tracked_since
// and every last_seen no later than as_of. A failure is an ErrBadRecord, and
// the caller treats the file as missing and reads the save again; a file from
// a newer format is also an ErrNewerFormat.
func Parse(b []byte, now time.Time) (Record, error) {
	if len(b) > MaxRecordBytes {
		return Record{}, fmt.Errorf("%w: %d bytes is over the %d byte limit", ErrBadRecord, len(b), MaxRecordBytes)
	}
	var head struct {
		FormatVersion int `json:"format_version"`
	}
	if json.Unmarshal(b, &head) == nil && head.FormatVersion > FormatVersion {
		return Record{}, fmt.Errorf("%w: %w (format %d)", ErrBadRecord, ErrNewerFormat, head.FormatVersion)
	}
	var r Record
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrBadRecord, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Record{}, fmt.Errorf("%w: data after the document", ErrBadRecord)
	}
	if r.Players == nil {
		r.Players = []Player{}
	}
	if err := r.validate(); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrBadRecord, err)
	}
	if r.AsOf.After(now.Add(MaxClockSkew)) {
		return Record{}, fmt.Errorf("%w: as_of is in the future", ErrBadRecord)
	}
	return r, nil
}

// Marshal writes the record as players.json: indented, with a final newline.
// It refuses a record Parse would refuse, so Foreman never writes a file it
// cannot read back.
func (r Record) Marshal() ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadRecord, err)
	}
	// Dates are written in UTC, and "players" is always an array.
	r.Basis.Modified = r.Basis.Modified.UTC()
	r.AsOf = r.AsOf.UTC()
	r.TrackedSince = r.TrackedSince.UTC()
	r.Seed.At = r.Seed.At.UTC()
	players := make([]Player, len(r.Players))
	for i, p := range r.Players {
		if p.LastSeen != nil {
			t := p.LastSeen.UTC()
			p.LastSeen = &t
		}
		players[i] = p
	}
	r.Players = players
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadRecord, err)
	}
	b = append(b, '\n')
	if len(b) > MaxRecordBytes {
		return nil, fmt.Errorf("%w: the record is %d bytes, over the %d byte limit", ErrBadRecord, len(b), MaxRecordBytes)
	}
	return b, nil
}

// validate checks every value of the record against the limits.
func (r Record) validate() error {
	if r.FormatVersion != FormatVersion {
		return fmt.Errorf("format_version is %d, not %d", r.FormatVersion, FormatVersion)
	}
	if !validSaveName(r.Basis.Save) || r.Basis.Size < 0 || r.Basis.Modified.IsZero() {
		return fmt.Errorf("basis is not a save")
	}
	if r.AsOf.IsZero() || r.TrackedSince.IsZero() || r.Seed.At.IsZero() {
		return fmt.Errorf("a date is missing")
	}
	if r.TrackedSince.After(r.AsOf) {
		return fmt.Errorf("tracked_since is after as_of")
	}
	if r.Seed.Result != ResultOK && !failureResult(r.Seed.Result) {
		return fmt.Errorf("seed result %q is not known", r.Seed.Result)
	}
	if !validSaveName(r.Seed.Save) {
		return fmt.Errorf("seed save is not a save name")
	}
	if r.Seed.GameVersion != "" && !validVersion(r.Seed.GameVersion) {
		return fmt.Errorf("seed game_version is odd")
	}
	if r.GameTicks < 0 || r.GameTicks > MaxWhole {
		return fmt.Errorf("game_ticks is out of range")
	}
	if len(r.Players) > MaxPlayers {
		return fmt.Errorf("more than %d players", MaxPlayers)
	}
	seen := make(map[string]bool, len(r.Players))
	for _, p := range r.Players {
		if err := validPlayerName(p.Name); err != nil {
			return fmt.Errorf("a player name is refused: %w", err)
		}
		key := asciiLower(p.Name)
		if seen[key] {
			return fmt.Errorf("player %q is listed twice", p.Name)
		}
		seen[key] = true
		if p.SeededTicks < 0 || p.SeededTicks > MaxWhole || p.TrackedMS < 0 || p.TrackedMS > MaxWhole {
			return fmt.Errorf("a time of %q is out of range", p.Name)
		}
		if p.LastSeen != nil && (p.LastSeen.IsZero() || p.LastSeen.After(r.AsOf)) {
			return fmt.Errorf("last_seen of %q is empty or after as_of", p.Name)
		}
	}
	return nil
}

// AddSession folds one player's session. It counts only the part of
// [from, to] after AsOf (nothing when to is at or before AsOf), sets the
// player's last seen to to (never to an earlier date than they already have),
// and adds the player when they are missing, even for a session of no length.
// A session that ends before it starts, and a name the game would not accept,
// change nothing.
//
// The part counted is exact to the millisecond: it is the whole milliseconds
// at its end less those at its start, so a play time cut into many folds, each
// one starting where the last ended, adds up to the same total as one fold.
//
// It does not move AsOf. The caller folds every open session up to one moment
// from the same starting AsOf, then sets AsOf to that moment; folding one
// session and moving AsOf before the next would drop the overlap of two
// players who were on together. Setting AsOf after the fold is also what
// makes a replayed log line count once. The caller must never pass two
// overlapping sessions of the same player in one fold: each is counted in
// full, so the overlap would count twice.
func (r *Record) AddSession(name string, from, to time.Time) {
	if to.Before(from) {
		return
	}
	p := r.player(name)
	if p == nil {
		return
	}
	if to.After(r.AsOf) {
		start := from
		if start.Before(r.AsOf) {
			start = r.AsOf
		}
		p.TrackedMS = min(p.TrackedMS+to.UnixMilli()-start.UnixMilli(), MaxWhole)
	}
	if p.LastSeen == nil || to.After(*p.LastSeen) {
		t := to.UTC()
		p.LastSeen = &t
	}
}

// player finds a player by name, comparing ASCII letters without regard to
// case (asciiLower), and adds them if they are new. It returns nil for a name the game would not
// accept, or when the list is full.
func (r *Record) player(name string) *Player {
	for i := range r.Players {
		if sameName(r.Players[i].Name, name) {
			return &r.Players[i]
		}
	}
	if len(r.Players) >= MaxPlayers || validPlayerName(name) != nil {
		return nil
	}
	r.Players = append(r.Players, Player{Name: name})
	return &r.Players[len(r.Players)-1]
}

// SetGameTicks sets the game's total play time in ticks, from the game's
// /time. A value outside 0 to 2^53 is ignored. The record keeps no date for
// it, so at is not stored; it is the caller's moment of asking. It does not
// move AsOf, which is about sessions.
func (r *Record) SetGameTicks(t int64, _ time.Time) {
	if t < 0 || t > MaxWhole {
		return
	}
	r.GameTicks = t
}

// Rebase points the record at a new save, keeping what was tracked: the
// players, TrackedSince, AsOf, Inexact and the seed's own facts do not
// change, since the new save is the same game saved later. The moment of the
// new save is the caller's, so at is not stored.
func (r *Record) Rebase(b Basis, _ time.Time) {
	b.Modified = b.Modified.UTC()
	r.Basis = b
}
