package gamesettings

import (
	"strings"
	"testing"
)

func TestReadVisibility(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Visibility
		ok   bool
	}{
		{"lan", `{"visibility": {"public": false, "lan": true}}`, VisibilityLAN, true},
		{"hidden", `{"visibility": {"public": false, "lan": false}}`, VisibilityHidden, true},
		{"public", `{"visibility": {"public": true, "lan": false}}`, VisibilityPublic, true},
		{"public and lan is public", `{"visibility": {"public": true, "lan": true}}`, VisibilityPublic, true},
		{"missing object", `{}`, "", false},
		{"missing key", `{"visibility": {"lan": true}}`, "", false},
		{"wrong type", `{"visibility": {"public": "no", "lan": true}}`, "", false},
		{"not an object", `{"visibility": true}`, "", false},
		{"not JSON", `{`, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ReadVisibility([]byte(tc.in))
			if got != tc.want || ok != tc.ok {
				t.Errorf("got %q %v", got, ok)
			}
		})
	}
}

func TestSetVisibility(t *testing.T) {
	in := "{\n  \"name\": \"a\",\n  \"visibility\": { \"public\": true, \"lan\": false },\n  \"tags\": [ \"x\" ]\n}\n"
	got, err := SetVisibility([]byte(in), VisibilityLAN)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"name\": \"a\",\n  \"visibility\": {\"public\": false, \"lan\": true},\n  \"tags\": [ \"x\" ]\n}\n"
	if string(got) != want {
		t.Errorf("got %q", got)
	}
	if v, ok := ReadVisibility(got); !ok || v != VisibilityLAN {
		t.Errorf("read back %q %v", v, ok)
	}

	got, err = SetVisibility([]byte(`{"name": "a"}`), VisibilityHidden)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := ReadVisibility(got); !ok || v != VisibilityHidden {
		t.Errorf("appended: read back %q %v from %s", v, ok, got)
	}
	if errs := withoutMissing(Validate(FileServerSettings, Line21, got)); len(errs) != 0 {
		t.Errorf("result does not validate: %+v", errs)
	}
}

func TestSetVisibilityRefuses(t *testing.T) {
	if _, err := SetVisibility([]byte(`{}`), VisibilityPublic); err == nil {
		t.Error("public was accepted")
	}
	if _, err := SetVisibility([]byte(`{}`), "other"); err == nil {
		t.Error("unknown value was accepted")
	}
	if _, err := SetVisibility([]byte(`[]`), VisibilityLAN); err == nil {
		t.Error("non-object was accepted")
	}
	big := []byte("{}" + strings.Repeat(" ", MaxBytes))
	if _, err := SetVisibility(big, VisibilityLAN); err == nil {
		t.Error("oversize input was accepted")
	}
}

func TestFormVisibilitiesExcludePublic(t *testing.T) {
	for _, v := range FormVisibilities {
		if v == VisibilityPublic {
			t.Error("the form must not offer public")
		}
	}
	if len(FormVisibilities) != 2 {
		t.Errorf("got %v", FormVisibilities)
	}
}
