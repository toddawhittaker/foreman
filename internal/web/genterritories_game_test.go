//go:build game

package web

// The Map preview's territories through the page's own routes, against the
// real 2.1.24 game (SPEC 5.6): Vulcanus, seed 123456 at 1280, lists 12 small
// and 6 medium territories, and the download draws them in. Run by `make
// test-game TAG=2.1`, never by `make verify`.

import (
	"bytes"
	"context"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGameVBTerritoriesPage(t *testing.T) {
	const version = "2.1.24"
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && only != "2.1" {
		t.Skip("territories are checked on 2.1.24")
	}
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	dc, err := docker.New(host)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dc.InspectImage(context.Background(), game.ImageRef(version)); err != nil {
		t.Fatalf("the game image %s is not on this host (%v). Pull it with docker pull, then run again", version, err)
	}
	data := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(data, "game.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	worlds, err := world.NewManager(world.Dirs{
		Worlds: filepath.Join(data, "worlds"), Snapshots: filepath.Join(data, "snapshots"), Imports: filepath.Join(data, "import"),
	}, st, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctrl, err := game.New(game.Config{
		Docker: dc, Worlds: worlds, Audit: st, HostDataDir: data, GameUID: os.Getuid(), GameGID: os.Getgid(),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	hn := newGenerateHarness(t, ctrl)
	c := hn.signIn()
	hn.withActive(version)
	hx := withHeader("HX-Request", "true")

	pic := hn.postLong(PathGeneratePreview, vulcanusForm("auto", "1"), c, hx).Body.String()
	if !strings.Contains(pic, `id="preview-territories"`) {
		t.Fatalf("no territory box beside Vulcanus:\n%s", pic)
	}
	body := hn.postLong(pathTerritories, vulcanusForm(), c, hx).Body.String()
	if !strings.Contains(body, "Demolisher territories in this picture: 12 small, 6 medium.") {
		t.Fatalf("the list is not 12 small and 6 medium:\n%s", body)
	}
	t.Logf("labels: %d", strings.Count(body, `class="territories__label"`))
	if n := strings.Count(body, `class="territories__label"`); n != 18 {
		t.Errorf("%d labels, want 18", n)
	}
	download := strings.Split(pictureOf(t, pic), `"`)[0] + "/download?territories=1"
	rec := hn.getLong(download, c)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status %d", rec.Code)
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil || img.Bounds().Dx() != 1280 {
		t.Fatalf("the download is not a 1280 picture: %v", err)
	}
}
