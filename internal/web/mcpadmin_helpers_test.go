package web

import (
	"crypto/sha256"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

var reHTMLTag = regexp.MustCompile(`<[^>]*>`)

// pageWords is an HTML body's text with tags gone and spaces made single,
// as a reader sees it.
func pageWords(body string) string {
	return strings.Join(strings.Fields(html.UnescapeString(reHTMLTag.ReplaceAllString(body, ""))), " ")
}

// pageText renders a page in English as the browser gets it and returns its
// words.
func pageText(t *testing.T, p Page) string {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := newTestRenderer(t).Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), 200, "alice", LiveState{}, nil, p); err != nil {
		t.Fatal(err)
	}
	return pageWords(rec.Body.String())
}

// wantWill is the preview parity test (SPEC 21.8): it fails unless will has
// sentences and each one appears in the words of the UI page.
func wantWill(t *testing.T, will []string, words string) {
	t.Helper()
	if len(will) == 0 {
		t.Fatal("the preview has no will sentences")
	}
	for _, w := range will {
		if want := strings.Join(strings.Fields(w), " "); !strings.Contains(words, want) {
			t.Errorf("the preview says %q, which the page does not:\n%s", want, words)
		}
	}
}

// dataDigest hashes every file name and content under the data folder, so a
// test can show that a preview changed nothing on disk (SPEC 21.8).
func dataDigest(t *testing.T, dirs world.Dirs) string {
	t.Helper()
	h := sha256.New()
	root := filepath.Dir(dirs.Worlds)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p) //nolint:gosec // a test temp folder
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s %d\n", rel, len(b))
		h.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// wantDataUnchanged fails unless the data folder and the audit log are as
// they were when digest and audits were taken.
func wantDataUnchanged(t *testing.T, g *gameRig, what, digest string, audits int) {
	t.Helper()
	if dataDigest(t, g.worlds.Dirs()) != digest {
		t.Errorf("%s changed the data folder", what)
	}
	if n := len(g.audit()); n != audits {
		t.Errorf("%s changed the audit log: %d rows, want %d", what, n, audits)
	}
}
