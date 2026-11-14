package web

import "testing"

// A figure and its unit are split so the unit can be drawn quieter
// (DESIGN 7.5); the text a reader gets is unchanged.
func TestFigureSplitsValueFromUnit(t *testing.T) {
	cases := map[string]string{
		"40.0%":      `<span class="fig">40.0</span><span class="unit">%</span>`,
		"40,0 %":     `<span class="fig">40,0</span> <span class="unit">%</span>`,
		"1.5 MB":     `<span class="fig">1.5</span> <span class="unit">MB</span>`,
		"400 B":      "<span class=\"fig\">400</span> <span class=\"unit\">B</span>",
		"12":         `<span class="fig">12</span>`,
		"<b>1 x</b>": `<span class="fig">&lt;b&gt;1</span> <span class="unit">x&lt;/b&gt;</span>`,
	}
	for in, want := range cases {
		if got := string(figure(in)); got != want {
			t.Errorf("figure(%q) = %q, want %q", in, got, want)
		}
	}
}

// A bar's width is a whole percent; a part that is not zero still shows,
// and two sizes near the top of the range do not overflow.
func TestShareIsAWholePercentThatStillShows(t *testing.T) {
	cases := []struct {
		part, rest uint64
		want       int
	}{
		{0, 0, 0},
		{0, 500, 0},
		{1, 1 << 40, 1},
		{25, 75, 25},
		{500, 0, 100},
		{1 << 63, 1 << 63, 50},
	}
	for _, c := range cases {
		if got := share(c.part, c.rest); got != c.want {
			t.Errorf("share(%d, %d) = %d, want %d", c.part, c.rest, got, c.want)
		}
	}
}
