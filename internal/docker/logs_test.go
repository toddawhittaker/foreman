package docker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func frame(st Stream, text string) []byte {
	hdr := make([]byte, 8)
	hdr[0] = byte(st)
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(text))) //nolint:gosec // a test line is short
	return append(hdr, text...)
}

func streamOf(parts ...[]byte) *LogStream {
	return newLogStream(io.NopCloser(bytes.NewReader(bytes.Join(parts, nil))))
}

func drain(t *testing.T, s *LogStream) []LogLine {
	t.Helper()
	var out []LogLine
	for {
		l, err := s.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, l)
	}
}

func TestLogStreamSplitsStdoutAndStderr(t *testing.T) {
	got := drain(t, streamOf(frame(Stdout, "one\n"), frame(Stderr, "oops\n"), frame(Stdout, "two\n")))
	want := []LogLine{{Stdout, "one"}, {Stderr, "oops"}, {Stdout, "two"}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestLogStreamJoinsAndSplitsFrames(t *testing.T) {
	got := drain(t, streamOf(
		frame(Stdout, "par"), frame(Stderr, "err\n"), frame(Stdout, "tial\nsecond\r\nthird"),
	))
	want := []LogLine{{Stderr, "err"}, {Stdout, "partial"}, {Stdout, "second"}, {Stdout, "third"}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestLogStreamKeepsEmptyLines(t *testing.T) {
	got := drain(t, streamOf(frame(Stdout, "a\n\nb\n")))
	if len(got) != 3 || got[1].Text != "" {
		t.Errorf("got %v", got)
	}
}

func TestLogStreamCutsAVeryLongLine(t *testing.T) {
	got := drain(t, streamOf(frame(Stdout, strings.Repeat("x", maxLine+10)+"\n")))
	if len(got) != 2 || len(got[0].Text) != maxLine || len(got[1].Text) != 10 {
		t.Errorf("got %d lines", len(got))
	}
}

func TestLogStreamRefusesWhatIsNotFramed(t *testing.T) {
	cases := map[string][]byte{
		"plain text (a terminal)": []byte("2026-01-01 hello from a tty\n"),
		"unknown stream":          frame(Stream(3), "x\n"),
		"stdin stream":            frame(Stream(0), "x\n"),
		"nonzero padding":         append([]byte{1, 9, 0, 0, 0, 0, 0, 1}, 'x'),
		"huge frame":              {1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff},
	}
	for name, b := range cases {
		if _, err := streamOf(b).Next(); err == nil || errors.Is(err, io.EOF) {
			t.Errorf("%s: want an error, got %v", name, err)
		}
	}
}

func TestLogStreamTruncatedIsAnErrorNotAnEnd(t *testing.T) {
	cut := frame(Stdout, "hello\n")
	for _, b := range [][]byte{cut[:4], cut[:8], cut[:10]} {
		_, err := streamOf(b).Next()
		if err == nil || errors.Is(err, io.EOF) {
			t.Errorf("truncated stream (%d bytes): got %v", len(b), err)
		}
	}
}

func TestLogStreamEmptyIsEOF(t *testing.T) {
	if _, err := streamOf().Next(); !errors.Is(err, io.EOF) {
		t.Errorf("got %v", err)
	}
}
