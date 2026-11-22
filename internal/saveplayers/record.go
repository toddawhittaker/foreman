package saveplayers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
// not know, every value checked. A failure is an ErrBadRecord; a file from a
// newer format is also an ErrNewerFormat.
func Parse(b []byte) (Record, error) {
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
		key := strings.ToLower(p.Name)
		if seen[key] {
			return fmt.Errorf("player %q is listed twice", p.Name)
		}
		seen[key] = true
		if p.SeededTicks < 0 || p.SeededTicks > MaxWhole || p.TrackedSeconds < 0 || p.TrackedSeconds > MaxWhole {
			return fmt.Errorf("a time of %q is out of range", p.Name)
		}
		if p.LastSeen != nil && p.LastSeen.IsZero() {
			return fmt.Errorf("last_seen of %q is empty", p.Name)
		}
	}
	return nil
}

// Session is one stretch a player was on.
type Session struct {
	Name     string
	From, To time.Time
}

// AddSession folds one player's session. It counts only the part after AsOf,
// then moves AsOf to the session's end, so replaying a log line never counts
// a session twice. A session that ends at or before AsOf, one that is empty
// or backwards, and one with a name the game would not accept change nothing.
//
// Two players on together must be folded in one AddSessions call: folded one
// after the other, the second would lose the time before the first one's end.
func (r *Record) AddSession(name string, from, to time.Time) {
	r.AddSessions([]Session{{Name: name, From: from, To: to}})
}

// AddSessions folds several sessions against the same AsOf, then moves AsOf
// to the latest end among them. A session counts from the later of its start
// and the old AsOf, rounded to the nearest second; the player's last seen
// moves to its end when that is later.
func (r *Record) AddSessions(ss []Session) {
	asOf := r.AsOf
	next := asOf
	for _, s := range ss {
		if !s.To.After(s.From) || !s.To.After(asOf) {
			continue
		}
		p := r.player(s.Name)
		if p == nil {
			continue
		}
		start := s.From
		if start.Before(asOf) {
			start = asOf
		}
		p.TrackedSeconds = min(p.TrackedSeconds+int64(s.To.Sub(start).Round(time.Second)/time.Second), MaxWhole)
		if p.LastSeen == nil || s.To.After(*p.LastSeen) {
			t := s.To.UTC()
			p.LastSeen = &t
		}
		if s.To.After(next) {
			next = s.To
		}
	}
	r.AsOf = next.UTC()
}

// player finds a player without regard to case, as the game compares names,
// and adds them if they are new. It returns nil for a name the game would not
// accept, or when the list is full.
func (r *Record) player(name string) *Player {
	for i := range r.Players {
		if strings.EqualFold(r.Players[i].Name, name) {
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
