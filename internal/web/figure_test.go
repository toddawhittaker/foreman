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
