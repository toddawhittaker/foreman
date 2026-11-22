package game

import (
	"context"
	"errors"
	"os"

	"github.com/toddawhittaker/foreman/internal/saveplayers"
)

// A save read is the fifth and sixth kind of one-off container (SPEC 8.16,
// 14.3, ADR 0030): the world's save is linked into a work folder and mounted
// read-only, the world's mods and a control-only helper mod are added, and the
// game runs it with --benchmark for a few ticks, no network. The helper
// writes JSON that the caller reads. Task A1 builds it; the sentinels and
// types below are the contract.

// The refusals of a save read, one for each failure id of a record's seed
// (SPEC 8.16). A caller maps them with the saveplayers.Result* constants.
var (
	ErrSaveCopyImageMissing = errors.New("the game image for this world's version is not downloaded")
	ErrSaveCopyOutOfMemory  = errors.New("the game ran out of memory reading the save")
	ErrSaveCopyTimeout      = errors.New("reading the save took too long")
	ErrSaveCopyModsDiffer   = errors.New("the mods the game loaded are not the world's mods")
	ErrSaveCopySaveNewer    = errors.New("the save was made by a newer game than the world's")
	ErrSaveCopyBadOutput    = errors.New("the game made no usable output")
)

// saveCopyState is the save reads in flight, by world id. Task A1 fills it.
type saveCopyState struct{} //nolint:unused // built by A1

// saveCopyMod is a control mod that runs against a copy of the world's save.
type saveCopyMod struct { //nolint:unused // built by A1
	controlMod // kind, name, dir, control, output, maxOutput, memory
	// outputOnDisk makes the game write its data in the work folder, not the
	// 64 MB tmpfs (the factory map).
	outputOnDisk bool
}

// SaveCopyResult is what a run of the save leaves: the save that was read and
// the helper's output.
type SaveCopyResult struct {
	Basis saveplayers.Basis // the save that was read
	// Output is the output file's bytes, or empty for outputOnDisk kinds,
	// whose files the caller's read function reads from the work folder.
	Output []byte
}

// saveCopyCommand is the shell script that runs the work folder's save with
// the control mod enabled, and leaves the mod's output files in the work
// folder (SPEC 8.16). It is the only place the seeding method lives.
func saveCopyCommand(m saveCopyMod) string { //nolint:unused // built by A1
	return ""
}

// runSaveCopy reads the save the world would load, in the preview slot. It
// calls read with the work folder's root and the run's folder while the files
// are there, and returns the basis of the save that was read, or an
// ErrSaveCopy* error.
func (c *Controller) runSaveCopy(ctx context.Context, id string, m saveCopyMod, //nolint:unused // built by A1
	read func(base *os.Root, dir string) error) (saveplayers.Basis, error) {
	return saveplayers.Basis{}, errors.New("not built")
}

// cancelSaveCopies kills every save read of the world that is running, so a
// start never races one (SPEC 8.16). It is called at the top of Start.
func (c *Controller) cancelSaveCopies(id string) {}
