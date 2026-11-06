package gamesettings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The example files shipped in the game images must pass for their own line
// (SPEC 11.2): a table that rejects the game's own file is wrong.
func TestExampleFilesValidate(t *testing.T) {
	for _, line := range []string{Line11, Line20, Line21} {
		for _, file := range []string{FileServerSettings, FileMapGen, FileMapSettings} {
			name := line + "-" + strings.TrimSuffix(file, ".json") + ".example.json"
			b, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // a fixed test fixture
			if err != nil {
				t.Fatal(err)
			}
			if errs := Validate(file, line, b); len(errs) != 0 {
				t.Errorf("%s: %+v", name, errs)
			}
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		file string
		line string
		in   string
		want []FieldError
	}{
		{name: "empty object is fine", file: FileServerSettings, line: Line21, in: `{}`},
		{name: "unknown keys are allowed", file: FileServerSettings, line: Line21,
			in: `{"mod_made_up": [1, {"a": null}], "_comment_x": "hi"}`},
		{name: "empty input", file: FileServerSettings, line: Line21, in: ``,
			want: []FieldError{{Line: 1, Code: CodeSyntax}}},
		{name: "only spaces", file: FileServerSettings, line: Line21, in: " \n\n ",
			want: []FieldError{{Line: 1, Code: CodeSyntax}}},
		{name: "array is not an object", file: FileServerSettings, line: Line21, in: "\n[1]",
			want: []FieldError{{Line: 2, Code: CodeNotObject}}},
		{name: "string is not an object", file: FileMapGen, line: Line20, in: `"x"`,
			want: []FieldError{{Line: 1, Code: CodeNotObject}}},
		{name: "number is not an object", file: FileMapGen, line: Line20, in: `7`,
			want: []FieldError{{Line: 1, Code: CodeNotObject}}},
		{name: "missing comma on line 3", file: FileServerSettings, line: Line21,
			in:   "{\n  \"name\": \"a\",\n  \"description\": \"b\"\n  \"max_players\": 3\n}",
			want: []FieldError{{Line: 4, Code: CodeSyntax}}},
		{name: "trailing comma on line 3", file: FileServerSettings, line: Line21,
			in:   "{\n  \"name\": \"a\",\n  \"auto_pause\": true,\n}",
			want: []FieldError{{Line: 4, Code: CodeSyntax}}},
		{name: "bad escape", file: FileServerSettings, line: Line21,
			in:   "{\n\n\"name\": \"a\\q\"}",
			want: []FieldError{{Line: 3, Code: CodeSyntax}}},
		{name: "truncated input points at the last line with text", file: FileServerSettings, line: Line21,
			in:   "{\n  \"name\": \"a\",\n  \"auto_pause\":\n\n\n",
			want: []FieldError{{Line: 3, Code: CodeSyntax}}},
		{name: "trailing text after the object", file: FileServerSettings, line: Line21,
			in:   "{}\n{}",
			want: []FieldError{{Line: 2, Code: CodeSyntax}}},
		{name: "trailing garbage after the object", file: FileServerSettings, line: Line21,
			in:   "{}\n\nx",
			want: []FieldError{{Line: 3, Code: CodeSyntax}}},
		{name: "comments are not JSON", file: FileServerSettings, line: Line21,
			in:   "{\n// no\n}",
			want: []FieldError{{Line: 2, Code: CodeSyntax}}},
		{name: "wrong type with line", file: FileServerSettings, line: Line21,
			in:   "{\n  \"name\": \"ok\",\n  \"auto_pause\": \"yes\"\n}",
			want: []FieldError{{Line: 3, Key: "auto_pause", Code: CodeWrongType, Want: "boolean"}}},
		{name: "nested key", file: FileServerSettings, line: Line21,
			in:   "{\n  \"visibility\": {\n    \"lan\": 1\n  }\n}",
			want: []FieldError{{Line: 3, Key: "visibility.lan", Code: CodeWrongType, Want: "boolean"}}},
		{name: "object where a string belongs", file: FileServerSettings, line: Line21,
			in:   `{"name": {}}`,
			want: []FieldError{{Line: 1, Key: "name", Code: CodeWrongType, Want: "string"}}},
		{name: "null is not a string", file: FileServerSettings, line: Line21,
			in:   `{"description": null}`,
			want: []FieldError{{Line: 1, Key: "description", Code: CodeWrongType, Want: "string"}}},
		{name: "array element", file: FileServerSettings, line: Line21,
			in:   "{\"tags\": [\"a\",\n 2]}",
			want: []FieldError{{Line: 2, Key: "tags[1]", Code: CodeWrongType, Want: "string"}}},
		{name: "tags not an array", file: FileServerSettings, line: Line21,
			in:   `{"tags": "a"}`,
			want: []FieldError{{Line: 1, Key: "tags", Code: CodeWrongType, Want: "array"}}},
		{name: "fraction where an integer belongs", file: FileServerSettings, line: Line21,
			in:   `{"autosave_interval": 2.5}`,
			want: []FieldError{{Line: 1, Key: "autosave_interval", Code: CodeWrongType, Want: "integer"}}},
		{name: "exponent is not an integer", file: FileServerSettings, line: Line21,
			in:   `{"autosave_slots": 1e1}`,
			want: []FieldError{{Line: 1, Key: "autosave_slots", Code: CodeWrongType, Want: "integer"}}},
		{name: "negative count", file: FileServerSettings, line: Line21,
			in:   `{"max_players": -1}`,
			want: []FieldError{{Line: 1, Key: "max_players", Code: CodeOutOfRange, Want: "0..4294967295"}}},
		{name: "count over 32 bits", file: FileServerSettings, line: Line21,
			in:   `{"max_players": 4294967296}`,
			want: []FieldError{{Line: 1, Key: "max_players", Code: CodeOutOfRange, Want: "0..4294967295"}}},
		{name: "heartbeats low", file: FileServerSettings, line: Line21,
			in:   `{"max_heartbeats_per_second": 5}`,
			want: []FieldError{{Line: 1, Key: "max_heartbeats_per_second", Code: CodeOutOfRange, Want: "6..240"}}},
		{name: "heartbeats high", file: FileServerSettings, line: Line21,
			in:   `{"max_heartbeats_per_second": 241}`,
			want: []FieldError{{Line: 1, Key: "max_heartbeats_per_second", Code: CodeOutOfRange, Want: "6..240"}}},
		{name: "heartbeats edges", file: FileServerSettings, line: Line21,
			in: `{"max_heartbeats_per_second": 6}`},
		{name: "allow_commands text", file: FileServerSettings, line: Line21,
			in: `{"allow_commands": "admins-only"}`},
		{name: "allow_commands bool", file: FileServerSettings, line: Line21,
			in: `{"allow_commands": false}`},
		{name: "allow_commands unknown text", file: FileServerSettings, line: Line21,
			in:   `{"allow_commands": "sometimes"}`,
			want: []FieldError{{Line: 1, Key: "allow_commands", Code: CodeOutOfRange, Want: "true|false|admins-only"}}},
		{name: "allow_commands number", file: FileServerSettings, line: Line21,
			in:   `{"allow_commands": 1}`,
			want: []FieldError{{Line: 1, Key: "allow_commands", Code: CodeWrongType, Want: "boolean or string"}}},
		{name: "two errors in order", file: FileServerSettings, line: Line21,
			in: "{\n  \"auto_pause\": 1,\n  \"name\": 2\n}",
			want: []FieldError{
				{Line: 2, Key: "auto_pause", Code: CodeWrongType, Want: "boolean"},
				{Line: 3, Key: "name", Code: CodeWrongType, Want: "string"},
			}},
		{name: "key only newer lines know is unchecked on 2.0", file: FileServerSettings, line: Line20,
			in: `{"auto_pause_when_players_connect": "x"}`},
		{name: "key newer lines know is checked on 2.1", file: FileServerSettings, line: Line21,
			in:   `{"auto_pause_when_players_connect": "x"}`,
			want: []FieldError{{Line: 1, Key: "auto_pause_when_players_connect", Code: CodeWrongType, Want: "boolean"}},
		},
		{name: "map-gen seed null", file: FileMapGen, line: Line21, in: `{"seed": null}`},
		{name: "map-gen seed number", file: FileMapGen, line: Line21, in: `{"seed": 123}`},
		{name: "map-gen seed text", file: FileMapGen, line: Line21, in: `{"seed": "abc"}`,
			want: []FieldError{{Line: 1, Key: "seed", Code: CodeWrongType, Want: "integer or null"}}},
		{name: "map-gen control frequency", file: FileMapGen, line: Line21,
			in: "{\"autoplace_controls\": {\n\"coal\": {\"frequency\": \"x\", \"size\": -1}}}",
			want: []FieldError{
				{Line: 2, Key: "autoplace_controls.coal.frequency", Code: CodeWrongType, Want: "number"},
				{Line: 2, Key: "autoplace_controls.coal.size", Code: CodeOutOfRange, Want: "0.."},
			}},
		{name: "map-gen control not an object", file: FileMapGen, line: Line21,
			in:   `{"autoplace_controls": {"coal": 1}}`,
			want: []FieldError{{Line: 1, Key: "autoplace_controls.coal", Code: CodeWrongType, Want: "object"}}},
		{name: "map-gen starting point", file: FileMapGen, line: Line21,
			in:   `{"starting_points": [{"x": 0, "y": "1"}]}`,
			want: []FieldError{{Line: 1, Key: "starting_points[0].y", Code: CodeWrongType, Want: "number"}}},
		{name: "map-gen cliff richness range", file: FileMapGen, line: Line21,
			in:   `{"cliff_settings": {"richness": 11}}`,
			want: []FieldError{{Line: 1, Key: "cliff_settings.richness", Code: CodeOutOfRange, Want: "0..10"}}},
		{name: "map-gen water is an old-line key", file: FileMapGen, line: Line21,
			in: `{"water": "lots"}`},
		{name: "map-gen water on 1.1", file: FileMapGen, line: Line11,
			in:   `{"water": "lots"}`,
			want: []FieldError{{Line: 1, Key: "water", Code: CodeWrongType, Want: "number"}}},
		{name: "map-settings bool", file: FileMapSettings, line: Line21,
			in:   `{"pollution": {"enabled": 1}}`,
			want: []FieldError{{Line: 1, Key: "pollution.enabled", Code: CodeWrongType, Want: "boolean"}}},
		{name: "map-settings steering gone on 2.1", file: FileMapSettings, line: Line21,
			in: `{"steering": 5}`},
		{name: "map-settings steering on 2.0", file: FileMapSettings, line: Line20,
			in:   `{"steering": {"default": {"radius": "a"}}}`,
			want: []FieldError{{Line: 1, Key: "steering.default.radius", Code: CodeWrongType, Want: "number"}}},
		{name: "map-settings overload list", file: FileMapSettings, line: Line11,
			in:   `{"path_finder": {"overload_levels": [0, "x"]}}`,
			want: []FieldError{{Line: 1, Key: "path_finder.overload_levels[1]", Code: CodeWrongType, Want: "number"}}},
		{name: "unknown line gets only the generic checks", file: FileServerSettings, line: "3.0",
			in: `{"auto_pause": "yes"}`},
		{name: "unknown file gets only the generic checks", file: "other.json", line: Line21,
			in: `{"auto_pause": "yes"}`},
		{name: "unknown line still refuses bad syntax", file: FileServerSettings, line: "3.0",
			in:   "{\n,}",
			want: []FieldError{{Line: 2, Code: CodeSyntax}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Validate(tc.file, tc.line, []byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestValidateSizeLimit(t *testing.T) {
	pad := strings.Repeat(" ", MaxBytes-2)
	atLimit := "{" + pad + "}"
	if len(atLimit) != MaxBytes {
		t.Fatalf("test input is %d bytes", len(atLimit))
	}
	if errs := Validate(FileServerSettings, Line21, []byte(atLimit)); len(errs) != 0 {
		t.Errorf("1 MB exactly was refused: %+v", errs)
	}
	over := atLimit + " "
	want := []FieldError{{Code: CodeTooLarge}}
	if got := Validate(FileServerSettings, Line21, []byte(over)); !reflect.DeepEqual(got, want) {
		t.Errorf("over the limit: got %+v", got)
	}
}

func TestValidateLongLineNumbers(t *testing.T) {
	// 5000 lines before the problem.
	in := "{\n" + strings.Repeat("\"_c\": 1,\n", 5000) + "\"auto_pause\": 7}"
	got := Validate(FileServerSettings, Line21, []byte(in))
	want := []FieldError{{Line: 5002, Key: "auto_pause", Code: CodeWrongType, Want: "boolean"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

func TestValidateDepthLimit(t *testing.T) {
	deep := strings.Repeat(`{"a":`, 40) + "1" + strings.Repeat("}", 40)
	got := Validate(FileServerSettings, Line21, []byte(deep))
	if len(got) != 1 || got[0].Code != CodeSyntax || got[0].Line != 1 {
		t.Errorf("got %+v", got)
	}
	arrays := strings.Repeat("[", 500000)
	got = Validate(FileServerSettings, Line21, []byte("{\"a\":"+arrays))
	if len(got) != 1 || got[0].Code != CodeSyntax {
		t.Errorf("got %+v", got)
	}
}

func TestValidateErrorCap(t *testing.T) {
	in := "{" + strings.Repeat(`"auto_pause": 1,`, 200) + `"name": "x"}`
	if got := Validate(FileServerSettings, Line21, []byte(in)); len(got) != maxErrors {
		t.Errorf("got %d errors", len(got))
	}
}

// A wrong value in a secret key is reported by type only (SPEC 14.8).
func TestValidateErrorsCarryNoValue(t *testing.T) {
	in := `{"game_password": 12345678, "token": {"s": "hunter2"}, "password": ["hunter3"]}`
	got := Validate(FileServerSettings, Line21, []byte(in))
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	for _, e := range got {
		for _, s := range []string{"12345678", "hunter2", "hunter3"} {
			if strings.Contains(e.Key+e.Code+e.Want, s) {
				t.Errorf("error holds a value: %+v", e)
			}
		}
	}
}
