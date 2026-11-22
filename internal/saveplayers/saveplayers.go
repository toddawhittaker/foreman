// Package saveplayers is Foreman's record of the players of a save: who
// joined it and for how long (SPEC 8.14). It holds no I/O: it parses the
// helper mod's output, the record file's bytes, and does the arithmetic. The
// record is the world's players.json (ADR 0030).
package saveplayers

import (
	"errors"
	"time"
)

// FormatVersion is the newest players.json format this Foreman knows.
const FormatVersion = 1

// The results a record's seed can have (SPEC 8.16). ResultOK is a read that
// worked; the others are the failure ids.
const (
	ResultOK           = "ok"
	ResultImageMissing = "image-missing"
	ResultOutOfMemory  = "out-of-memory"
	ResultTimeout      = "timeout"
	ResultModsDiffer   = "mods-differ"
	ResultSaveNewer    = "save-newer"
	ResultBadOutput    = "bad-output"
)

// Limits of the helper mod's output (SPEC 14.6).
const (
	MaxOutputBytes = 1 << 20
	MaxPlayers     = 10000
	MaxMods        = 1000
	// MaxWhole is the biggest whole number the output may hold, 2^53.
	MaxWhole = 1 << 53
)

var errNotBuilt = errors.New("not built")

// Basis is the save a record matches: its name, size and modification time.
// The record is valid while the save the world would load is this one.
type Basis struct {
	Save     string    `json:"save"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Mod is a mod's name and version, as the helper reports its active mods.
type Mod struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// SeedPlayer is one player in the helper mod's output.
type SeedPlayer struct {
	Name        string `json:"name"`
	OnlineTicks int64  `json:"online_ticks"`
}

// Seed is the helper mod's output (saveplayers.json, format 1).
type Seed struct {
	GameVersion string
	Tick        int64
	Mods        []Mod
	Players     []SeedPlayer
}

// SeedInfo says how the record was last read from a save.
type SeedInfo struct {
	At          time.Time `json:"at"`
	Save        string    `json:"save"`
	GameVersion string    `json:"game_version"`
	Result      string    `json:"result"`
}

// Player is one player in a record.
type Player struct {
	Name           string     `json:"name"`
	SeededTicks    int64      `json:"seeded_ticks"`
	TrackedSeconds int64      `json:"tracked_seconds"`
	LastSeen       *time.Time `json:"last_seen"`
}

// Record is players.json.
type Record struct {
	FormatVersion int       `json:"format_version"`
	Basis         Basis     `json:"basis"`
	AsOf          time.Time `json:"as_of"`
	Seed          SeedInfo  `json:"seed"`
	TrackedSince  time.Time `json:"tracked_since"`
	Inexact       bool      `json:"inexact"`
	GameTicks     int64     `json:"game_ticks"`
	Players       []Player  `json:"players"`
}

// ParseSeed reads the helper mod's output under the limits of SPEC 14.6.
func ParseSeed(b []byte) (Seed, error) { return Seed{}, errNotBuilt }

// FromSeed makes the record a good read gives.
func FromSeed(s Seed, b Basis, at time.Time) Record { return Record{} }

// Failed makes the record a failed read gives: tracked from at, with no
// seeded totals. result is one of the failure ids.
func Failed(result string, b Basis, at time.Time) Record { return Record{} }

// Parse reads players.json bytes.
func Parse(b []byte) (Record, error) { return Record{}, errNotBuilt }

// Marshal writes the record as players.json.
func (r Record) Marshal() ([]byte, error) { return nil, errNotBuilt }

// AddSession folds one player's session. It counts only the part after AsOf,
// then moves AsOf, so replaying a log line never counts a session twice.
func (r *Record) AddSession(name string, from, to time.Time) {}

// SetGameTicks sets the game's total play time, true at the moment at.
func (r *Record) SetGameTicks(t int64, at time.Time) {}

// Rebase points the record at a new save, keeping what was tracked.
func (r *Record) Rebase(b Basis, at time.Time) {}
