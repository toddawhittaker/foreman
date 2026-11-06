package gamesettings

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const fullFile = `{
  "name": "My game",
  "game_password": "hunter2",
  "tags": [ "a", "b" ],
  "username": "todd",
  "password": "",
  "visibility": { "public": false, "lan": true },
  "token": "tok-123",
  "extra_unknown": { "x": 1 }
}
`

func TestStripSecrets(t *testing.T) {
	got, set := StripSecrets([]byte(fullFile))
	want := `{
  "name": "My game",
  "tags": [ "a", "b" ],
  "visibility": { "public": false, "lan": true },
  "extra_unknown": { "x": 1 }
}
`
	if string(got) != want {
		t.Errorf("stripped:\n%s\nwant:\n%s", got, want)
	}
	for _, s := range []string{"hunter2", "todd", "tok-123", "game_password", "username", "token"} {
		if strings.Contains(string(got), s) {
			t.Errorf("stripped text still holds %q", s)
		}
	}
	wantSet := map[string]bool{"game_password": true, "username": true, "token": true}
	if !reflect.DeepEqual(set, wantSet) {
		t.Errorf("set = %v", set)
	}
}

func TestStripSecretsLayouts(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"only secrets", `{"token":"a"}`, `{}`},
		{"compact", `{"name":"x","token":"a","tags":[]}`, `{"name":"x","tags":[]}`},
		{"secret first", "{\n  \"token\": \"a\",\n  \"name\": \"x\"\n}", "{\n  \"name\": \"x\"\n}"},
		{"secret last", "{\n  \"name\": \"x\",\n  \"token\": \"a\"\n}", "{\n  \"name\": \"x\"\n}"},
		{"adjacent", `{"password":"a","token":"b","name":"x"}`, `{"name":"x"}`},
		{"secret with object value", `{"token":{"s":"hunter2"},"name":"x"}`, `{"name":"x"}`},
		{"none", "{\"name\": \"x\"}\n", "{\"name\": \"x\"}\n"},
		{"repeated key", `{"token":"a","name":"x","token":"b"}`, `{"name":"x"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := StripSecrets([]byte(tc.in))
			if string(got) != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
			if !json.Valid(got) {
				t.Errorf("result is not JSON: %q", got)
			}
		})
	}
}

func TestStripSecretsRefusesWhatItCannotParse(t *testing.T) {
	for _, in := range []string{``, `{"game_password": "hunter2"`, `[1]`, `{"game_password": "hunter2",}`} {
		got, set := StripSecrets([]byte(in))
		if got != nil || len(set) != 0 || set == nil {
			t.Errorf("%q: got %q, %v", in, got, set)
		}
	}
	big := []byte(`{"game_password":"x"}` + strings.Repeat(" ", MaxBytes))
	if got, _ := StripSecrets(big); got != nil {
		t.Error("oversize input was returned")
	}
}

// Saving what the editor showed keeps every secret (SPEC 11.4).
func TestSecretsRoundTrip(t *testing.T) {
	stripped, _ := StripSecrets([]byte(fullFile))
	merged := MergeSecrets(stripped, []byte(fullFile))
	if !json.Valid(merged) {
		t.Fatalf("not JSON: %s", merged)
	}
	if got := Diff([]byte(fullFile), merged); len(got) != 0 {
		t.Errorf("round trip changed the file: %+v\n%s", got, merged)
	}
	// The password key was an empty string, which is "not set", so it is not restored.
	var m map[string]any
	if err := json.Unmarshal(merged, &m); err != nil {
		t.Fatal(err)
	}
	if m["game_password"] != "hunter2" || m["username"] != "todd" || m["token"] != "tok-123" {
		t.Errorf("secrets lost: %v", m)
	}
	// Order of what was kept is the editor's order.
	if strings.Index(string(merged), `"name"`) > strings.Index(string(merged), `"visibility"`) {
		t.Errorf("order changed:\n%s", merged)
	}
}

func TestMergeSecrets(t *testing.T) {
	current := `{"name": "a", "game_password": "old", "token": "t"}`
	tests := []struct {
		name, edited, want string
	}{
		{"restores at the end", `{"name": "b"}`, `{"name": "b","game_password": "old","token": "t"}`},
		{"restores into an empty object", `{}`, "{\n  \"game_password\": \"old\",\n  \"token\": \"t\"\n}"},
		{"keeps layout", "{\n  \"name\": \"b\"\n}\n", "{\n  \"name\": \"b\",\n  \"game_password\": \"old\",\n  \"token\": \"t\"\n}\n"},
		{"edited value wins", `{"game_password": "new"}`, `{"game_password": "new","token": "t"}`},
		{"empty string clears", `{"game_password": "", "token": null}`, `{"game_password": "", "token": null}`},
		{"nothing to restore", `{"game_password": "x", "token": "y"}`, `{"game_password": "x", "token": "y"}`},
		{"invalid edit is returned as is", `{"name": `, `{"name": `},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(MergeSecrets([]byte(tc.edited), []byte(current)))
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestMergeSecretsBadCurrent(t *testing.T) {
	for _, cur := range []string{``, `{"game_password": `, `[]`} {
		if got := MergeSecrets([]byte(`{"a": 1}`), []byte(cur)); string(got) != `{"a": 1}` {
			t.Errorf("current %q: got %q", cur, got)
		}
	}
}

// A secret with an awkward value is carried across byte for byte (compacted).
func TestMergeSecretsKeepsOddValues(t *testing.T) {
	current := `{"game_password": "quote\" and \\ and é"}`
	got := MergeSecrets([]byte(`{}`), []byte(current))
	var m map[string]string
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("%s: %v", got, err)
	}
	if m["game_password"] != "quote\" and \\ and é" {
		t.Errorf("got %q", m["game_password"])
	}
}

func TestStripSecretsOnExampleFile(t *testing.T) {
	b, err := os.ReadFile("testdata/2.1-server-settings.example.json")
	if err != nil {
		t.Fatal(err)
	}
	stripped, set := StripSecrets(b)
	if len(set) != 0 {
		t.Errorf("example holds empty secrets, set = %v", set)
	}
	if errs := Validate(FileServerSettings, Line21, stripped); len(errs) != 0 {
		t.Errorf("stripped example no longer validates: %+v", errs)
	}
	if !strings.Contains(string(stripped), "_comment_credentials") {
		t.Error("comment keys were lost")
	}
}
