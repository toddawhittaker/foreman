package saveplayers

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const goodSeed = `{"format": 1, "game_version": "2.0.77", "tick": 8352001,
 "active_mods": [{"name": "base", "version": "2.0.77"}, {"name": "space-age", "version": "2.0.77"}],
 "players": [{"name": "OrcScorpion", "online_ticks": 28484153}, {"name": "toddawhittaker", "online_ticks": 0}]}`

func TestParseSeedGood(t *testing.T) {
	s, err := ParseSeed([]byte(goodSeed))
	if err != nil {
		t.Fatal(err)
	}
	if s.GameVersion != "2.0.77" || s.Tick != 8352001 {
		t.Fatalf("header: %+v", s)
	}
	if len(s.Mods) != 2 || s.Mods[1] != (Mod{Name: "space-age", Version: "2.0.77"}) {
		t.Fatalf("mods: %+v", s.Mods)
	}
	if len(s.Players) != 2 || s.Players[0] != (SeedPlayer{Name: "OrcScorpion", OnlineTicks: 28484153}) {
		t.Fatalf("players: %+v", s.Players)
	}
}

func TestParseSeedEmptyPlayersIsFine(t *testing.T) {
	s, err := ParseSeed([]byte(`{"format":1,"game_version":"2.0.77","tick":0,"active_mods":[{"name":"base","version":"2.0.77"}],"players":[]}`))
	if err != nil || len(s.Players) != 0 {
		t.Fatalf("%v %+v", err, s)
	}
}

// A float that holds a whole number is a whole number: the game's JSON
// writer may print 8282296.0 or 8.282296e6.
func TestParseSeedAcceptsWholeFloats(t *testing.T) {
	s, err := ParseSeed([]byte(`{"format":1,"game_version":"2.0.77","tick":8.352001e6,"active_mods":[{"name":"base","version":"2.0.77"}],"players":[{"name":"a","online_ticks":12.0}]}`))
	if err != nil || s.Tick != 8352001 || s.Players[0].OnlineTicks != 12 {
		t.Fatalf("%v %+v", err, s)
	}
}

func seedWith(players, mods, header string) string {
	return fmt.Sprintf(`{"format":1,"game_version":"2.0.77","tick":1,%s"active_mods":%s,"players":%s}`, header, mods, players)
}

func TestParseSeedRefuses(t *testing.T) {
	base := `[{"name":"base","version":"2.0.77"}]`
	manyPlayers := "[" + strings.Repeat(`{"name":"a","online_ticks":1},`, MaxPlayers) + `{"name":"b","online_ticks":1}]`
	manyMods := "[" + strings.Repeat(`{"name":"m","version":"1.0.0"},`, MaxMods) + `{"name":"n","version":"1.0.0"}]`
	cases := map[string]string{
		"empty":               ``,
		"not json":            `nope`,
		"array":               `[]`,
		"trailing data":       goodSeed + ` {}`,
		"two documents":       goodSeed + goodSeed,
		"format 2":            strings.Replace(goodSeed, `"format": 1`, `"format": 2`, 1),
		"format missing":      `{"game_version":"2.0.77","tick":1,"active_mods":` + base + `,"players":[]}`,
		"unknown field":       strings.Replace(goodSeed, `"tick"`, `"extra": 1, "tick"`, 1),
		"unknown player key":  seedWith(`[{"name":"a","online_ticks":1,"x":2}]`, base, ""),
		"unknown mod key":     seedWith(`[]`, `[{"name":"base","version":"2.0.77","x":1}]`, ""),
		"tick missing":        `{"format":1,"game_version":"2.0.77","active_mods":` + base + `,"players":[]}`,
		"players missing":     `{"format":1,"game_version":"2.0.77","tick":1,"active_mods":` + base + `}`,
		"players null":        seedWith(`null`, base, ""),
		"mods missing":        `{"format":1,"game_version":"2.0.77","tick":1,"players":[]}`,
		"version missing":     `{"format":1,"tick":1,"active_mods":` + base + `,"players":[]}`,
		"version odd":         strings.Replace(goodSeed, `"game_version": "2.0.77"`, `"game_version": "2.0.77; rm"`, 1),
		"negative tick":       strings.Replace(goodSeed, `8352001`, `-1`, 1),
		"fraction tick":       strings.Replace(goodSeed, `8352001`, `1.5`, 1),
		"tick as string":      strings.Replace(goodSeed, `8352001`, `"7"`, 1),
		"tick over 2^53":      strings.Replace(goodSeed, `8352001`, `9007199254740993`, 1),
		"ticks over 2^53":     strings.Replace(goodSeed, `28484153`, `9007199254740993`, 1),
		"ticks negative":      strings.Replace(goodSeed, `28484153`, `-5`, 1),
		"ticks huge exponent": strings.Replace(goodSeed, `28484153`, `1e30`, 1),
		"name empty":          seedWith(`[{"name":"","online_ticks":1}]`, base, ""),
		"name with space":     seedWith(`[{"name":"a b","online_ticks":1}]`, base, ""),
		"name with control":   seedWith(`[{"name":"a\u0007b","online_ticks":1}]`, base, ""),
		"name invisible":      seedWith(`[{"name":"a\u200bb","online_ticks":1}]`, base, ""),
		"name too long":       seedWith(`[{"name":"`+strings.Repeat("a", 65)+`","online_ticks":1}]`, base, ""),
		"name not utf8":       seedWith(`[{"name":"a\udc00","online_ticks":1}]`, base, ""),
		"duplicate player":    seedWith(`[{"name":"a","online_ticks":1},{"name":"a","online_ticks":2}]`, base, ""),
		"duplicate by case":   seedWith(`[{"name":"Ab","online_ticks":1},{"name":"aB","online_ticks":2}]`, base, ""),
		"too many players":    seedWith(manyPlayers, base, ""),
		"too many mods":       seedWith(`[]`, manyMods, ""),
		"mod name bad":        seedWith(`[]`, `[{"name":"../x","version":"1.0.0"}]`, ""),
		"mod name empty":      seedWith(`[]`, `[{"name":"","version":"1.0.0"}]`, ""),
		"mod version bad":     seedWith(`[]`, `[{"name":"x","version":"v1\n"}]`, ""),
		"duplicate mod":       seedWith(`[]`, `[{"name":"x","version":"1.0.0"},{"name":"x","version":"1.0.0"}]`, ""),
		"over 1 MiB":          goodSeed + strings.Repeat(" ", MaxOutputBytes),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSeed([]byte(in))
			if err == nil {
				t.Fatal("accepted")
			}
			if !errors.Is(err, ErrBadSeed) {
				t.Fatalf("error does not wrap ErrBadSeed: %v", err)
			}
		})
	}
}

// The limits are inclusive: exactly the maximum is read.
func TestParseSeedLimitsInclusive(t *testing.T) {
	base := `{"name":"base","version":"2.0.77"}`
	var mods, players []string
	mods = append(mods, base)
	for i := 1; i < MaxMods; i++ {
		mods = append(mods, fmt.Sprintf(`{"name":"m%d","version":"1.0.0"}`, i))
	}
	for i := 0; i < MaxPlayers; i++ {
		players = append(players, fmt.Sprintf(`{"name":"p%d","online_ticks":%d}`, i, int64(MaxWhole)))
	}
	in := fmt.Sprintf(`{"format":1,"game_version":"2.0.77","tick":%d,"active_mods":[%s],"players":[%s]}`,
		int64(MaxWhole), strings.Join(mods, ","), strings.Join(players, ","))
	if len(in) > MaxOutputBytes {
		t.Fatalf("test input is %d bytes, over the limit", len(in))
	}
	s, err := ParseSeed([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Mods) != MaxMods || len(s.Players) != MaxPlayers || s.Tick != MaxWhole {
		t.Fatalf("%d mods, %d players, tick %d", len(s.Mods), len(s.Players), s.Tick)
	}
}
