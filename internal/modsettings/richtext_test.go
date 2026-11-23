package modsettings

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStripRichText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"[entity=iron-ore] Iron ore", "Iron ore"},
		{"[img=item/coal][item=coal]Coal", "Coal"},
		{"[color=red]Hot[/color] rocks", "Hot rocks"},
		{"[font=default-bold]Bold[/font]", "Bold"},
		{"Plain text", "Plain text"},
		{"Keep [not a tag] text", "Keep [not a tag] text"},
		{"  spaced   out  ", "spaced out"},
		{"[entity=x]", ""},
		{"Ünïcode [virtual-signal=signal-A] ok", "Ünïcode ok"},
		// Direction overrides, zero-width and other format or control
		// characters cannot reorder or hide a label (SPEC 14.6).
		{"a\u202Eb\u202Ac\u202Dd", "abcd"},
		{"x\u2066y\u2069z\u200Ew\u200F", "xyzw"},
		{"zero\u200Bwidth\u200C\u200D\uFEFFjoin", "zerowidthjoin"},
		{"nul\x00l\x07bell\u0085tail", "nullbell tail"},
		{"tab\tsplit\nlines", "tab split lines"},
		{"[ent\u200Bity=x]Name", "Name"},
		{"soft\u00ADhyphen \u2060joiner", "softhyphen joiner"},
	}
	for _, c := range cases {
		if got := StripRichText(c.in); got != c.want {
			t.Errorf("StripRichText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripRichTextCutsToLimit(t *testing.T) {
	got := StripRichText("[entity=a]" + strings.Repeat("é", 500))
	if n := utf8.RuneCountInString(got); n != MaxLabelChars {
		t.Fatalf("got %d characters, want %d", n, MaxLabelChars)
	}
	if !utf8.ValidString(got) {
		t.Fatal("cut left invalid UTF-8")
	}
}

// obj is how an object tag with a valid prototype name renders: its readable
// name in a span that carries the wiki image file (SPEC 14.10).
func obj(file, text string) string {
	return `<span class="rt-obj" data-wiki="` + file + `">` + text + `</span>`
}

// TestRichTextHTML pins the safe subset of the game's rich text that mod
// text keeps (SPEC 14.6).
func TestRichTextHTML(t *testing.T) {
	icons := RichTextIcons{Warning: "Warning", Info: "Note"}
	cases := []struct {
		name, in  string
		multiline bool
		want      string
	}{
		{"bold font", "[font=default-bold]B[/font]", false, "<strong>B</strong>"},
		{"heading font", "[font=heading-2]H[/font]", false, "<strong>H</strong>"},
		{"other bold font", "[font=count-bold]C[/font]", false, "<strong>C</strong>"},
		{"mod font", "[font=moshine-data]D[/font]", false, "D"},
		{"red", "[color=red]R[/color]", false, `<span class="rt-danger">R</span>`},
		{"orange", "[color=orange]O[/color]", false, `<span class="rt-warn">O</span>`},
		{"yellow", "[color=Yellow]Y[/color]", false, `<span class="rt-warn">Y</span>`},
		{"green", "[color=green]G[/color]", false, `<span class="rt-ok">G</span>`},
		{"blue", "[color=blue]B[/color]", false, `<span class="rt-info">B</span>`},
		{"cyan", "[color=cyan]C[/color]", false, `<span class="rt-info">C</span>`},
		{"purple", "[color=purple]P[/color]", false, `<span class="rt-info">P</span>`},
		{"pink", "[color=pink]P[/color]", false, `<span class="rt-info">P</span>`},
		{"gray", "[color=gray]G[/color]", false, `<span class="rt-muted">G</span>`},
		{"white", "[color=white]W[/color]", false, "W"},
		{"hex", "[color=#fbff6b]X[/color]", false, "X"},
		{"rgb", "[color=1,0.5,0]X[/color]", false, "X"},
		{"muluna", "[color=red][font=heading-2]Hard[/font][/color] Restrict", false,
			`<span class="rt-danger"><strong>Hard</strong></span> Restrict`},
		{"item", "requires [item=space-science-pack]", false, "requires " + obj("Space_science_pack", "Space science pack")},
		{"technology", "[technology=interstellar-science-pack]", false, obj("Interstellar_science_pack", "Interstellar science pack")},
		{"planet", "[planet=muluna]", false, obj("Muluna", "Muluna")},
		{"fluid", "[fluid=heavy-oil]", false, obj("Heavy_oil", "Heavy oil")},
		{"entity", "[entity=iron-ore]", false, obj("Iron_ore", "Iron ore")},
		{"recipe", "[recipe=a_b]", false, obj("A_b", "A b")},
		{"space location", "[space-location=solar-system-edge]", false, obj("Solar_system_edge", "Solar system edge")},
		{"virtual signal", "[virtual-signal=signal-A]", false, "Signal A"},
		{"tile", "[tile=landfill]", false, obj("Landfill", "Landfill")},
		{"warning img", "[img=utility/warning_icon] Hot", false,
			`<span class="rt-icon rt-icon--warning" role="img" aria-label="Warning"></span> Hot`},
		{"info img", "[img=utility/info]", false, `<span class="rt-icon rt-icon--info" role="img" aria-label="Note"></span>`},
		{"other utility img", "[img=utility/side_menu]x", false, "x"},
		{"object img", "[img=item/iron-plate]", false, obj("Iron_plate", "Iron plate")},
		{"bare img", "[img=thing]x", false, "x"},
		{"space age", "[space-age] only", false, "Space Age only"},
		{"escaped newline, multiline", `a\nb`, true, "a<br>b"},
		{"real newline, multiline", "a\nb", true, "a<br>b"},
		{"escaped newline, one line", `a\nb`, false, "a b"},
		{"control", "Press __CONTROL__toggle-driving__", false, `Press <span class="rt-key">Toggle driving</span>`},
		{"alt control", "Press __ALT_CONTROL__1__toggle-driving__ now", false, `Press <span class="rt-key">Toggle driving</span> now`},
		{"control click", "__CONTROL_LEFT_CLICK__build__", false, `<span class="rt-key">Build</span>`},
		{"other macro", "on __PLANET__nauvis__ x __1__", false, "on Nauvis x "},
		{"macro with text after", "Mined on __PLANET__nauvis__ only", false, "Mined on Nauvis only"},
		{"object with quality", "[item=iron-plate,quality=rare]", false, obj("Iron_plate", "Iron plate")},
		{"object with only parameters", "[item=,quality=rare]", false, "[item=,quality=rare]"},
		{"dot img", "[img=item.iron-plate]", false, obj("Iron_plate", "Iron plate")},
		{"dot img with extra dot", "[img=item.iron.plate]x", false, "x"},
		{"dot utility img", "[img=utility.warning_icon]", false, `<span class="rt-icon rt-icon--warning" role="img" aria-label="Warning"></span>`},
		{"nested", "[color=red]a[font=default-bold]b[/font]c[/color]", false,
			`<span class="rt-danger">a<strong>b</strong>c</span>`},
		{"unclosed closes at the end", "[color=red][font=default-bold]x", false,
			`<span class="rt-danger"><strong>x</strong></span>`},
		{"wrong nesting stays literal", "[color=red]a[/font]b[/color]", false,
			`<span class="rt-danger">a[/font]b</span>`},
		{"lone close", "x[/color]", false, "x[/color]"},
		{"lone bracket", "a [ b", false, "a [ b"},
		{"number in brackets", "[1]", false, "[1]"},
		{"unknown tag", "[gps=1,2]", false, "[gps=1,2]"},
		{"empty object", "[item=]", false, "[item=]"},
		{"hostile colour", `[color=red" onmouseover="alert(1)]x[/color]`, false, "x"},
		{"script", "<script>alert(1)</script>", false, "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{"hostile object", `[item=<b>"x"</b>]`, false, "&lt;b&gt;&#34;x&#34;&lt;/b&gt;"},
		{"hostile control", "__CONTROL__<x>__", false, "__CONTROL__&lt;x&gt;__"},
		{"invisible", "a\u202Eb", false, "ab"},
	}
	for _, c := range cases {
		if got := string(RichTextHTML(c.in, c.multiline, icons)); got != c.want {
			t.Errorf("%s: RichTextHTML(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
