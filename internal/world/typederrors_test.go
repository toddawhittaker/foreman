package world

import (
	"errors"
	"fmt"
	"testing"
)

// The typed errors read as the sentences the logs have always had, match
// their sentinel through wrapping, and carry what a page needs to explain
// them in another language.
func TestTypedErrorsKeepTheirTextAndSentinels(t *testing.T) {
	for _, c := range []struct {
		err      error
		sentinel error
		text     string
	}{
		{&NoSpaceError{Need: 4e9, Reserve: 2e9, Free: 1e9}, ErrNoSpace,
			"not enough free disk space: this needs 4.0 GB, including 2.0 GB kept free for the game, but only 1.0 GB is free. Free some space and try again"},
		{&DamagedError{Kind: DamageBadPath, Path: "../x"}, ErrDamaged, ErrDamaged.Error() + `: bad path "../x"`},
		{&DamagedError{Kind: DamageMissing, Path: "a"}, ErrDamaged, ErrDamaged.Error() + ": a is missing"},
		{&DamagedError{Kind: DamageChanged, Path: "a"}, ErrDamaged, ErrDamaged.Error() + ": a has changed"},
		{&DamagedError{Kind: DamageChangedSince, Path: "a"}, ErrDamaged, ErrDamaged.Error() + ": a has changed since the snapshot was taken"},
		{&UnsupportedFileError{Path: "mods/link"}, ErrUnsupportedFile,
			ErrUnsupportedFile.Error() + ": mods/link is a symlink or special file, which a snapshot cannot keep. Remove it or replace it with a copy, then try again"},
		{SaveClashError{Name: "a.zip"}, ErrSaveClash,
			"the save a.zip is in both the folder and its saves folder, so it is not clear which to import. Remove or rename one of them"},
	} {
		wrapped := fmt.Errorf("while doing it: %w", c.err)
		if !errors.Is(wrapped, c.sentinel) {
			t.Errorf("%T does not match its sentinel", c.err)
		}
		if got := c.err.Error(); got != c.text {
			t.Errorf("%T: %q, want %q", c.err, got, c.text)
		}
	}

	var ns *NoSpaceError
	if err := checkSpace(1e9, 0, 3e9); !errors.As(err, &ns) || ns.Need != 3e9+SpaceReserve || ns.Reserve != SpaceReserve || ns.Free != 1e9 {
		t.Errorf("checkSpace: %v, %+v", err, ns)
	}
}
