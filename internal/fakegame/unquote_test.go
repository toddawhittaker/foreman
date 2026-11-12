package fakegame

import "testing"

func TestUnquote(t *testing.T) {
	for in, want := range map[string]string{
		`/factorio/config/x.json`:   `/factorio/config/x.json`,
		`"/factorio/config/x.json"`: `/factorio/config/x.json`,
		`'/factorio/config/x.json'`: `/factorio/config/x.json`,
		`"/factorio/config/x.json`:  `"/factorio/config/x.json`,
		`"`:                         `"`,
	} {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%s) = %s, want %s", in, got, want)
		}
	}
}
