package web

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each group's file starts with its card marker and links the stylesheet
// copied beside it, with the fonts the stylesheet loads (DESIGN 7).
func TestExportDesign(t *testing.T) {
	dir := t.TempDir()
	if err := ExportDesign(dir); err != nil {
		t.Fatal(err)
	}
	for _, g := range designGroups {
		f, err := os.Open(filepath.Join(dir, g.Key+".html"))
		if err != nil {
			t.Fatal(err)
		}
		s := bufio.NewScanner(f)
		s.Scan()
		if want := `<!-- @dsCard group="` + g.Card + `" -->`; s.Text() != want {
			t.Errorf("%s starts %q, want %q", g.Key, s.Text(), want)
		}
		var rest strings.Builder
		for s.Scan() {
			rest.WriteString(s.Text())
		}
		_ = f.Close()
		if !strings.Contains(rest.String(), `<link rel="stylesheet" href="foreman.css">`) || !strings.Contains(rest.String(), `data-theme="dark"`) {
			t.Errorf("%s does not link foreman.css or lacks a dark pane", g.Key)
		}
	}
	for _, name := range []string{"foreman.css", "README.md", "vendor/ibm-plex-sans-latin-wght-normal.woff2", "vendor/IBM-Plex-Sans-OFL.txt", "vendor/LUCIDE-LICENSE.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Error(err)
		}
	}
}
