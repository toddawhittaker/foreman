package main

import (
	"os"
	"regexp"
	"testing"
)

// The images Foreman runs are the ones the real-game tests check.
func TestGameImagesMatchMakefile(t *testing.T) {
	b, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"GAME_IMAGE_1_1": gameImage11, "GAME_IMAGE_2_0": gameImage20, "GAME_IMAGE_2_1": gameImage21}
	for name, value := range want {
		m := regexp.MustCompile(`(?m)^` + name + `\s*:=\s*(\S+)\s*$`).FindSubmatch(b)
		if m == nil {
			t.Errorf("the Makefile has no %s", name)
			continue
		}
		if string(m[1]) != value {
			t.Errorf("%s is %s in the Makefile but %s in main.go", name, m[1], value)
		}
	}
	if len(gameImages) != len(want) {
		t.Errorf("gameImages has %d entries, want %d", len(gameImages), len(want))
	}
}
