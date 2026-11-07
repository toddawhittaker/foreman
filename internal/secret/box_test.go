package secret

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const testKey = "keykeykeykeykeykeykeykeykeykeykey"

func newBox(t *testing.T, key string) *Box {
	t.Helper()
	b, err := NewBox(New(key))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBoxRoundTrip(t *testing.T) {
	b := newBox(t, testKey)
	sealed, err := b.Seal("row", []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	if sealed[0] != 0x01 || bytes.Contains(sealed, []byte("hunter2")) {
		t.Errorf("sealed value has the wrong shape: %x", sealed)
	}
	got, err := b.Open("row", sealed)
	if err != nil || string(got) != "hunter2" {
		t.Errorf("Open = %q, %v", got, err)
	}
	again, _ := b.Seal("row", []byte("hunter2"))
	if bytes.Equal(sealed, again) {
		t.Error("two seals of one value are identical, so the nonce is not fresh")
	}
}

func TestBoxShortKey(t *testing.T) {
	if _, err := NewBox(New(strings.Repeat("k", MinKeyLength-1))); !errors.Is(err, ErrKeyTooShort) {
		t.Errorf("err = %v, want ErrKeyTooShort", err)
	}
}

func TestBoxRefusals(t *testing.T) {
	b := newBox(t, testKey)
	sealed, _ := b.Seal("row", []byte("value"))

	flipped := bytes.Clone(sealed)
	flipped[len(flipped)-1] ^= 1
	badVersion := bytes.Clone(sealed)
	badVersion[0] = 0x02

	for name, tc := range map[string]struct {
		box    *Box
		row    string
		sealed []byte
		want   error
	}{
		"wrong key":    {newBox(t, strings.Repeat("z", 40)), "row", sealed, ErrCannotOpen},
		"moved row":    {b, "other", sealed, ErrCannotOpen},
		"tampered":     {b, "row", flipped, ErrCannotOpen},
		"unknown ver":  {b, "row", badVersion, ErrBadSealed},
		"empty":        {b, "row", nil, ErrBadSealed},
		"version only": {b, "row", []byte{0x01}, ErrBadSealed},
		"truncated":    {b, "row", sealed[:20], ErrBadSealed},
		"garbage":      {b, "row", bytes.Repeat([]byte{1}, 64), ErrCannotOpen},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := tc.box.Open(tc.row, tc.sealed)
			if !errors.Is(err, tc.want) || got != nil {
				t.Errorf("Open = %q, %v; want %v", got, err, tc.want)
			}
		})
	}
}
