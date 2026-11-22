// Package saveplayers is Foreman's record of the players of a save: who
// joined it and for how long (SPEC 8.14). It holds no I/O: it parses the
// helper mod's output and the record file's bytes, and does the arithmetic.
// The record is the world's players.json (ADR 0030).
//
// Both inputs are untrusted. The helper mod runs in a one-off container that
// can write its work folder, and the game container can write the world
// folder where players.json lives, so each parse is under the limits of
// SPEC 14.6 and refuses what it does not know.
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

// Limits of the helper mod's output and of the record (SPEC 14.6).
const (
	MaxOutputBytes = 1 << 20
	MaxPlayers     = 10000
	MaxMods        = 1000
	// MaxWhole is the biggest whole number the output may hold, 2^53.
	MaxWhole = 1 << 53
	// MaxRecordBytes is the biggest players.json that is parsed. It is the
	// limit the world package reads under.
	MaxRecordBytes = 4 << 20
)

// TicksPerSecond is the game's tick rate at normal speed.
const TicksPerSecond = 60

var (
	// ErrBadSeed means the helper mod's output is not a good read: it is
	// too big, not format 1, or holds a value outside the limits. The
	// caller records it as ResultBadOutput.
	ErrBadSeed = errors.New("the save reader's output is not valid")
	// ErrBadRecord means players.json is not a record this Foreman can use.
	// The caller treats the record as missing.
	ErrBadRecord = errors.New("the players record is not valid")
	// ErrNewerFormat means players.json was written by a newer Foreman. It
	// also is an ErrBadRecord; callers may tell the two apart to leave the
	// file alone.
	ErrNewerFormat = errors.New("the players record is from a newer Foreman")
)

// Basis is the save a record matches: its name, size and modification time.
// The record is valid while the save the world would load is this one.
type Basis struct {
	Save     string    `json:"save"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Same says whether two bases name the same save: the same file name and
// size, and the same modification time to the nanosecond.
func (b Basis) Same(o Basis) bool {
	return b.Save == o.Save && b.Size == o.Size && b.Modified.Equal(o.Modified)
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

// Player is one player in a record. Their play time is the ticks the read
// found plus the seconds Foreman tracked since (PlayedSeconds).
type Player struct {
	Name           string     `json:"name"`
	SeededTicks    int64      `json:"seeded_ticks"`
	TrackedSeconds int64      `json:"tracked_seconds"`
	LastSeen       *time.Time `json:"last_seen"`
}

// PlayedSeconds is the player's whole play time: the read's ticks, at the
// game's 60 a second, plus the time Foreman tracked.
func (p Player) PlayedSeconds() int64 {
	return p.SeededTicks/TicksPerSecond + p.TrackedSeconds
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

// Seeded says whether the record starts from a good read of a save. A record
// that does not is tracked since TrackedSince and has no earlier totals.
func (r Record) Seeded() bool { return r.Seed.Result == ResultOK }

// GameSeconds is the game's total play time, from GameTicks.
func (r Record) GameSeconds() int64 { return r.GameTicks / TicksPerSecond }
