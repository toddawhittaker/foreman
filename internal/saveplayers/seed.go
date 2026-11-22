package saveplayers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
)

// The helper mod's output is format 1.
const seedFormat = 1

// maxVersionBytes and maxModNameBytes bound the text fields of the output.
const (
	maxVersionBytes = 32
	maxModNameBytes = 100
	maxSaveBytes    = 255
)

// seedJSON is the output as written. Every field is required, so a field the
// mod forgot is a bad read, not a zero. Numbers stay raw until wholeNumber
// has checked them.
type seedJSON struct {
	Format      json.RawMessage `json:"format"`
	GameVersion *string         `json:"game_version"`
	Tick        json.RawMessage `json:"tick"`
	ActiveMods  []modJSON       `json:"active_mods"`
	Players     []seedPlayerRaw `json:"players"`
}

type modJSON struct {
	Name    *string `json:"name"`
	Version *string `json:"version"`
}

type seedPlayerRaw struct {
	Name        *string         `json:"name"`
	OnlineTicks json.RawMessage `json:"online_ticks"`
}

// ParseSeed reads the helper mod's output under the limits of SPEC 14.6: at
// most 1 MiB, 10,000 players and 1,000 mods, whole numbers from 0 to 2^53,
// player names by the rule of 8.2, and no field it does not know. A failure
// is an ErrBadSeed.
func ParseSeed(b []byte) (Seed, error) {
	s, err := parseSeed(b)
	if err != nil {
		return Seed{}, fmt.Errorf("%w: %w", ErrBadSeed, err)
	}
	return s, nil
}

func parseSeed(b []byte) (Seed, error) {
	if len(b) > MaxOutputBytes {
		return Seed{}, fmt.Errorf("%d bytes is over the %d byte limit", len(b), MaxOutputBytes)
	}
	var raw seedJSON
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Seed{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return Seed{}, fmt.Errorf("data after the document")
	}
	if f, ok := wholeNumber(raw.Format); !ok || f != seedFormat {
		return Seed{}, fmt.Errorf("format is not %d", seedFormat)
	}
	var s Seed
	if raw.GameVersion == nil || !validVersion(*raw.GameVersion) {
		return Seed{}, fmt.Errorf("game_version is missing or odd")
	}
	s.GameVersion = *raw.GameVersion
	var ok bool
	if s.Tick, ok = wholeNumber(raw.Tick); !ok {
		return Seed{}, fmt.Errorf("tick is missing or not a whole number from 0 to 2^53")
	}
	if raw.ActiveMods == nil || raw.Players == nil {
		return Seed{}, fmt.Errorf("active_mods or players is missing")
	}
	if len(raw.ActiveMods) > MaxMods {
		return Seed{}, fmt.Errorf("more than %d mods", MaxMods)
	}
	if len(raw.Players) > MaxPlayers {
		return Seed{}, fmt.Errorf("more than %d players", MaxPlayers)
	}
	seenMod := make(map[string]bool, len(raw.ActiveMods))
	for _, m := range raw.ActiveMods {
		if m.Name == nil || m.Version == nil || !validModName(*m.Name) || !validVersion(*m.Version) {
			return Seed{}, fmt.Errorf("a mod has a missing or odd name or version")
		}
		if seenMod[*m.Name] {
			return Seed{}, fmt.Errorf("mod %q is listed twice", *m.Name)
		}
		seenMod[*m.Name] = true
		s.Mods = append(s.Mods, Mod{Name: *m.Name, Version: *m.Version})
	}
	seenPlayer := make(map[string]bool, len(raw.Players))
	for _, p := range raw.Players {
		if p.Name == nil {
			return Seed{}, fmt.Errorf("a player has no name")
		}
		if err := validPlayerName(*p.Name); err != nil {
			return Seed{}, fmt.Errorf("a player name is refused: %w", err)
		}
		key := asciiLower(*p.Name)
		if seenPlayer[key] {
			return Seed{}, fmt.Errorf("player %q is listed twice", *p.Name)
		}
		seenPlayer[key] = true
		t, ok := wholeNumber(p.OnlineTicks)
		if !ok {
			return Seed{}, fmt.Errorf("online_ticks of %q is missing or not a whole number from 0 to 2^53", *p.Name)
		}
		s.Players = append(s.Players, SeedPlayer{Name: *p.Name, OnlineTicks: t})
	}
	return s, nil
}

// wholeNumber reads a JSON number that is a whole number from 0 to 2^53. A
// float that holds a whole number (12.0, 8.3e6) counts, since the game's JSON
// writer may print one; a string, a fraction or a negative does not.
func wholeNumber(raw json.RawMessage) (int64, bool) {
	s := string(bytes.TrimSpace(raw))
	if s == "" || s[0] != '-' && (s[0] < '0' || s[0] > '9') {
		return 0, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, n >= 0 && n <= MaxWhole
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f < 0 || f > MaxWhole {
		return 0, false
	}
	return int64(f), true
}

// asciiLower lowers the ASCII letters of a name and nothing else, the rule of
// SPEC 12.11 for usernames: other letters compare exactly, so no two names
// that differ in a non-ASCII letter ever join. It is the one case rule for
// player names, used by every duplicate check and lookup.
func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// sameName says whether two player names are one player: equal once ASCII
// letters are lowered.
func sameName(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// validPlayerName applies the player name rule of SPEC 8.2. It also refuses
// U+FFFD, which is what a decoder leaves in place of bytes that were not
// UTF-8: the name was changed on the way in, and Foreman never changes a
// name to make it pass.
func validPlayerName(s string) error {
	if strings.ContainsRune(s, utf8.RuneError) {
		return errors.New("the name has a replacement character")
	}
	return gamesettings.ValidateAdminName(s)
}

// validVersion accepts a game or mod version: two to four numbers joined by
// dots, such as 2.0.77.
func validVersion(s string) bool {
	if s == "" || len(s) > maxVersionBytes {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range []byte(p) {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// validModName accepts the characters the mod portal allows in a mod name.
func validModName(s string) bool {
	if s == "" || len(s) > maxModNameBytes {
		return false
	}
	for _, c := range []byte(s) {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == ' ' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

// validSaveName accepts a file name of a save: text with no path separator
// and no control character.
func validSaveName(s string) bool {
	if s == "" || len(s) > maxSaveBytes || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
