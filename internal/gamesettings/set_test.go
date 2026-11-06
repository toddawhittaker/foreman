package gamesettings

import "testing"

func TestSetFieldsKeepsLayoutAndOrder(t *testing.T) {
	in := "{\n  \"name\": \"a\", \"_comment\": \"x\",\n  \"max_players\": 0,\n  \"game_password\": \"\"\n}\n"
	got, err := SetFields([]byte(in), []Field{
		{"max_players", []byte(`12`)},
		{"description", []byte(`"hi \"there\""`)},
		{"auto_pause", []byte(`true`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"name\": \"a\", \"_comment\": \"x\",\n  \"max_players\": 12,\n  \"game_password\": \"\",\n  \"description\": \"hi \\\"there\\\"\",\n  \"auto_pause\": true\n}\n"
	if string(got) != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if errs := Validate(FileServerSettings, Line21, got); len(errs) != 0 {
		t.Errorf("result does not validate: %+v", errs)
	}
}

func TestSetFieldsOnEmptyObject(t *testing.T) {
	got, err := SetFields([]byte(`{}`), []Field{{"name", []byte(`"n"`)}, {"max_players", []byte(`3`)}})
	if err != nil {
		t.Fatal(err)
	}
	if errs := Validate(FileServerSettings, Line21, got); len(errs) != 0 {
		t.Errorf("result does not validate: %+v\n%s", errs, got)
	}
	if ch := Diff([]byte(`{}`), got); len(ch) != 2 {
		t.Errorf("changes: %+v", ch)
	}
}

func TestSetFieldsRefuses(t *testing.T) {
	if _, err := SetFields([]byte(`[1]`), []Field{{"a", []byte(`1`)}}); err == nil {
		t.Error("an array was accepted")
	}
	if _, err := SetFields([]byte(`{`), []Field{{"a", []byte(`1`)}}); err == nil {
		t.Error("broken JSON was accepted")
	}
	if _, err := SetFields([]byte(`{}`), []Field{{"a", []byte(`{nope`)}}); err == nil {
		t.Error("a value that is not JSON was accepted")
	}
}

func TestUnknownKey(t *testing.T) {
	tests := []struct {
		line, key string
		want      bool
	}{
		{Line21, "name", false},
		{Line21, "visibility.lan", false},
		{Line21, "visibility", false},
		{Line21, "_comment_max_players", true},
		{Line21, "visibility.extra", true},
		{Line21, "mystery", true},
		{Line20, "auto_pause_when_players_connect", true},
		{Line21, "auto_pause_when_players_connect", false},
		{"9.9", "mystery", false},
		{"", "mystery", false},
	}
	for _, tc := range tests {
		if got := UnknownKey(FileServerSettings, tc.line, tc.key); got != tc.want {
			t.Errorf("%s %q: got %v", tc.line, tc.key, got)
		}
	}
	if UnknownKey("other.json", Line21, "x") {
		t.Error("a file without a table has no unknown keys")
	}
}
