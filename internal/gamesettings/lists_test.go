package gamesettings

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateReason(t *testing.T) {
	for _, ok := range []string{"", "griefing", "a \"quoted\" /reason", strings.Repeat("ü", MaxReasonChars)} {
		if err := ValidateReason(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"a\nb", "a\rb", "a\x00b", "\xff", strings.Repeat("a", MaxReasonChars+1), "x\n/ban other", "\ttab", "a\u0085b", "a\u2028b", "a\u2029b", "a\x1bb", "a\u007fb"} {
		if err := ValidateReason(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParseBanListGameFormat(t *testing.T) {
	// As the game writes it: a leading empty line, blank lines before objects.
	in := "\n[\n  \"carl\",\n  \n  {\n    \"username\": \"bob\",\n    \"reason\": \"griefing\"\n  },\n  \"CARL\"\n]\n"
	got, err := ParseBanList([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Ban{{Name: "carl"}, {Name: "bob", Reason: "griefing"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	back, err := ParseBanList(BanListJSON(got))
	if err != nil || !reflect.DeepEqual(back, want) {
		t.Errorf("round trip: %v %v\n%s", back, err, BanListJSON(got))
	}
	if s := string(BanListJSON(nil)); s != "[]\n" {
		t.Errorf("empty: %q", s)
	}
}

func TestParseBanListRefuses(t *testing.T) {
	bad := map[string]string{
		"object file":    `{"a": 1}`,
		"number":         `[1]`,
		"array element":  `[["a"]]`,
		"no username":    `[{"reason": "x"}]`,
		"space in name":  `["Bob Smith"]`,
		"control":        `[{"username": "a\u0000", "reason": "x"}]`,
		"too long":       `["` + strings.Repeat("a", MaxNameBytes+1) + `"]`,
		"trailing comma": `["a",]`,
	}
	for name, in := range bad {
		if got, err := ParseBanList([]byte(in)); err == nil {
			t.Errorf("%s: accepted %v", name, got)
		}
	}
}

func TestParseWhitelist(t *testing.T) {
	got, err := ParseWhitelist([]byte(`["hank", "ivy", "Hank"]`))
	if err != nil || !reflect.DeepEqual(got, []string{"hank", "ivy"}) {
		t.Errorf("got %v %v", got, err)
	}
	if _, err := ParseWhitelist([]byte(`["a b"]`)); err == nil {
		t.Error("a name with a space was accepted")
	}
	back, err := ParseWhitelist(NameListJSON(got))
	if err != nil || !reflect.DeepEqual(back, got) {
		t.Errorf("round trip: %v %v", back, err)
	}
}
