package web

import (
	"strings"
	"testing"
)

func TestNewMapPathFillsTheRoute(t *testing.T) {
	if got, want := newMapPath("abc"), strings.Replace(PathNewMap, "{id}", "abc", 1); got != want {
		t.Errorf("newMapPath = %q, want %q", got, want)
	}
}
