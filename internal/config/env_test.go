package config

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func fakeEnv(vars map[string]string, files map[string]string) env {
	return env{
		getenv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok },
		readFile: func(p string) ([]byte, error) {
			if c, ok := files[p]; ok {
				return []byte(c), nil
			}
			return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
		},
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		name    string
		vars    map[string]string
		files   map[string]string
		want    string
		wantOK  bool
		wantErr []string // substrings
	}{
		{name: "neither", vars: nil},
		{name: "value", vars: map[string]string{"X": "plain"}, want: "plain", wantOK: true},
		{name: "empty value is unset", vars: map[string]string{"X": ""}},
		{name: "file", vars: map[string]string{"X_FILE": "/run/s"}, files: map[string]string{"/run/s": "from-file"}, want: "from-file", wantOK: true},
		{name: "file trailing newline", vars: map[string]string{"X_FILE": "/run/s"}, files: map[string]string{"/run/s": "from-file\n"}, want: "from-file", wantOK: true},
		{name: "file trailing CRLF", vars: map[string]string{"X_FILE": "/run/s"}, files: map[string]string{"/run/s": "from-file\r\n"}, want: "from-file", wantOK: true},
		{name: "file keeps inner spaces", vars: map[string]string{"X_FILE": "/run/s"}, files: map[string]string{"/run/s": " a b \n"}, want: " a b ", wantOK: true},
		{name: "empty file var is unset", vars: map[string]string{"X_FILE": ""}},
		{
			name:    "both set",
			vars:    map[string]string{"X": "a", "X_FILE": "/run/s"},
			files:   map[string]string{"/run/s": "b"},
			wantErr: []string{"X and X_FILE are both set", "remove one"},
		},
		{
			name:    "missing file",
			vars:    map[string]string{"X_FILE": "/run/nope"},
			wantErr: []string{"X_FILE", "/run/nope"},
		},
		{
			name:    "empty file",
			vars:    map[string]string{"X_FILE": "/run/s"},
			files:   map[string]string{"/run/s": "\n"},
			wantErr: []string{"X_FILE names an empty file"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := fakeEnv(tt.vars, tt.files)
			got, ok, err := e.lookup("X")
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("want an error containing %q, got value %q", tt.wantErr, got)
				}
				for _, s := range tt.wantErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not contain %q", err, s)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("lookup = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestLookupMissingFileKeepsCause(t *testing.T) {
	_, _, err := fakeEnv(map[string]string{"X_FILE": "/run/nope"}, nil).lookup("X")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %v should wrap fs.ErrNotExist", err)
	}
}

func TestLookupSecretRedacts(t *testing.T) {
	e := fakeEnv(map[string]string{"X_FILE": "/run/s"}, map[string]string{"/run/s": "topsecret\n"})
	s, ok, err := e.lookupSecret("X")
	if err != nil || !ok {
		t.Fatalf("lookupSecret = %v, %v", ok, err)
	}
	if s.Reveal() != "topsecret" {
		t.Errorf("Reveal() = %q", s.Reveal())
	}
	if got := s.String(); strings.Contains(got, "topsecret") {
		t.Errorf("secret prints as %q", got)
	}

	_, ok, err = fakeEnv(nil, nil).lookupSecret("X")
	if err != nil || ok {
		t.Errorf("unset secret = %v, %v", ok, err)
	}

	_, _, err = fakeEnv(map[string]string{"X": "a", "X_FILE": "/p"}, nil).lookupSecret("X")
	if err == nil {
		t.Error("both set should fail")
	}
}
