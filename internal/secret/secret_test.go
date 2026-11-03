package secret_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"git.thewhittakers.org/todd/foreman/internal/secret"
)

const plain = "hunter2-very-secret"

type holder struct {
	Name  string        `json:"name"`
	Token secret.Secret `json:"token"`
}

func TestRevealAndIsZero(t *testing.T) {
	if got := secret.New(plain).Reveal(); got != plain {
		t.Errorf("Reveal() = %q, want %q", got, plain)
	}
	if secret.New(plain).IsZero() {
		t.Error("IsZero() = true for a non-empty secret")
	}
	if !secret.New("").IsZero() || !(secret.Secret{}).IsZero() {
		t.Error("IsZero() = false for an empty secret")
	}
}

func TestPrintingRedacts(t *testing.T) {
	s := secret.New(plain)
	h := holder{Name: "x", Token: s}
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"String", s.String(), "[redacted]"},
		{"GoString", s.GoString(), "[redacted]"},
		{"%v", fmt.Sprintf("%v", s), "[redacted]"},
		{"%+v", fmt.Sprintf("%+v", s), "[redacted]"},
		{"%#v", fmt.Sprintf("%#v", s), "[redacted]"},
		{"%s", fmt.Sprintf("%s", s), "[redacted]"},
		{"%q", fmt.Sprintf("%q", s), "[redacted]"},
		{"%x", fmt.Sprintf("%x", s), "[redacted]"},
		{"%d", fmt.Sprintf("%d", s), "[redacted]"},
		{"pointer %v", fmt.Sprintf("%v", &s), "[redacted]"},
		{"struct %v", fmt.Sprintf("%v", h), "{x [redacted]}"},
		{"struct %+v", fmt.Sprintf("%+v", h), "{Name:x Token:[redacted]}"},
		{"struct %#v", fmt.Sprintf("%#v", h), `secret_test.holder{Name:"x", Token:[redacted]}`},
		{"Println", strings.TrimSpace(fmt.Sprintln(s)), "[redacted]"},
		{"error wrap", fmt.Errorf("bad value %v", s).Error(), "bad value [redacted]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
			if strings.Contains(tt.got, plain) {
				t.Errorf("output %q leaks the secret", tt.got)
			}
		})
	}
}

func TestJSONRedacts(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"bare", secret.New(plain), `"[redacted]"`},
		{"pointer", &holder{Name: "x", Token: secret.New(plain)}, `{"name":"x","token":"[redacted]"}`},
		{"struct", holder{Name: "x", Token: secret.New(plain)}, `{"name":"x","token":"[redacted]"}`},
		{"map", map[string]secret.Secret{"k": secret.New(plain)}, `{"k":"[redacted]"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.want {
				t.Errorf("got %s, want %s", b, tt.want)
			}
		})
	}
}

func TestMarshalText(t *testing.T) {
	b, err := secret.New(plain).MarshalText()
	if err != nil || string(b) != "[redacted]" {
		t.Errorf("MarshalText() = %q, %v", b, err)
	}
}

func TestSlogRedacts(t *testing.T) {
	s := secret.New(plain)
	h := holder{Name: "x", Token: s}
	handlers := map[string]func(*bytes.Buffer) slog.Handler{
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	}
	for name, mk := range handlers {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(mk(&buf))
			log.Info("message", "token", s, "ptr", &s, "holder", h, slog.Group("g", slog.Any("token", s)))
			log.With("token", s).Info("with")
			out := buf.String()
			if strings.Contains(out, plain) {
				t.Errorf("log output leaks the secret: %s", out)
			}
			if !strings.Contains(out, "[redacted]") {
				t.Errorf("log output has no redaction marker: %s", out)
			}
		})
	}
}
