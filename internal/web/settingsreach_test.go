package web

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/secret"
)

// The General settings reach the pages they govern (SPEC 7.6, 14.6, 14.10).

// setSetting stores an app setting as the settings page does.
func (hn *harness) setSetting(name string, value int) {
	hn.t.Helper()
	if err := hn.store.SetAppSetting(context.Background(), name, strconv.Itoa(value), "alice", hn.clock.Now()); err != nil {
		hn.t.Fatal(err)
	}
}

// SPEC 7.6: the stop, restart and switch forms show the countdown setting,
// and a request that leaves the field empty gets it too.
func TestCountdownSettingShowsOnTheForms(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.startRunning()
	b := g.newWorld("Other", "2.1.24-no-space-age.zip", false)
	g.setSetting("stop_countdown", 45)
	for name, path := range map[string]string{
		"stop": gamePath(a, "stop"), "restart": gamePath(a, "restart"), "switch": switchPath(b),
	} {
		body := g.get(path)
		if !strings.Contains(body, `name="countdown"`) || !strings.Contains(body, `value="45"`) || strings.Contains(body, `value="60"`) {
			t.Errorf("%s form does not show the setting 45:\n%s", name, body)
		}
	}
	g.setSetting("stop_countdown", 0)
	if body := g.get(gamePath(a, "stop")); !strings.Contains(body, `name="countdown" type="text" inputmode="numeric" value="0"`) {
		t.Errorf("a setting of 0 is not shown:\n%s", body)
	}
}

// SPEC 7.6: an empty countdown field means the setting; a typed one wins.
func TestReadCountdownUsesTheSetting(t *testing.T) {
	hn := newHarness(t, nil)
	hn.setSetting("stop_countdown", 30)
	s := hn.generalServer()
	for _, c := range []struct {
		field string
		want  time.Duration
	}{{"", 30 * time.Second}, {"90", 90 * time.Second}, {"0", 0}} {
		r, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{"countdown": {c.field}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if _, got := s.readCountdown(r); got.Countdown != c.want {
			t.Errorf("field %q: countdown %v, want %v", c.field, got.Countdown, c.want)
		}
	}
}

// SPEC 7.6, 21.5: an MCP stop with no countdown given warns for the setting.
func TestMCPStopUsesTheCountdownSetting(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeOperate, image21)
	id := g.startRunning()
	g.join(id, "bob")
	g.setSetting("stop_countdown", 120)
	was := mcpWait
	mcpWait = 50 * time.Millisecond
	t.Cleanup(func() { mcpWait = was })
	asked := time.Now()
	if res, out := call(t, cs, "stop_server", map[string]any{"world_id": id}); res.IsError || out["finished"] != false {
		t.Fatalf("stop: %s %s", resultText(res), asJSON(out))
	}
	var ends time.Time
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if op := g.ctrl.Status(id).Operation; op != nil {
			if e, ok := op.CountdownEnds(); ok {
				ends = e
				break
			}
		}
	}
	if d := ends.Sub(asked); d < 115*time.Second || d > 125*time.Second {
		t.Errorf("countdown ends %v after the call, want about 120s", d)
	}
	if err := g.ctrl.CancelStop(context.Background(), id, game.Actor{Username: "alice"}); err != nil {
		t.Fatal(err)
	}
}

// postDeclared sends a multipart upload by hand whose Content-Length is
// declared, which may be far more than the body that follows, so a limit
// of hundreds of megabytes is tested without sending them.
func (g *gameRig) postDeclared(path, field string, declared int64) reply {
	g.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("name", "Imported")
	_ = mw.WriteField("game_version", "2.0.72")
	fw, _ := mw.CreateFormFile(field, "x.zip")
	_, _ = fw.Write([]byte("not a zip"))
	_ = mw.Close()
	u, _ := url.Parse(g.url)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		g.t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n",
		path, u.Host, g.cookie.String(), mw.FormDataContentType(), declared)
	_, _ = conn.Write([]byte(head))
	_, _ = conn.Write(b.Bytes())
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return reply{}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, body: string(body)}
}

// uploadRefusedAsTooLarge reports whether the reply is the size refusal.
func uploadRefusedAsTooLarge(r reply) bool {
	return strings.Contains(r.body, "larger than the limit of")
}

// SPEC 14.6: an upload over a lowered limit is refused by the server, and
// the refusal says the new figure. The limit is read for each request.
func TestLoweredUploadLimitIsEnforced(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	g.setSetting("save_upload_mb", 100)
	g.setSetting("mod_upload_mb", 10)
	for _, c := range []struct {
		name, path, field, limit string
		over                     int64
	}{
		{"save", worldPath(id) + "/saves", "save", "100 MB", 100<<20 + 1},
		{"import", PathImportUpload, "save", "100 MB", 100<<20 + 1},
		{"mod", worldPath(id) + "/mods/upload", "mod", "10 MB", 10<<20 + 1},
	} {
		got := g.postDeclared(c.path, c.field, c.over)
		if got.StatusCode < 400 || got.StatusCode >= 500 || !strings.Contains(got.body, "limit of "+c.limit) {
			t.Errorf("%s at %d bytes: status %d, want a refusal naming %s:\n%.400s", c.name, c.over, got.StatusCode, c.limit, got.body)
		}
	}
}

// SPEC 14.6: a raised limit lets a bigger upload through the size check, and
// the figure the page and the refusal say follows it.
func TestRaisedUploadLimitIsEnforced(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	// 1 GB and 200 MB are the defaults: an upload one byte over is refused.
	if got := g.postDeclared(worldPath(id)+"/saves", "save", 1<<30+1); !uploadRefusedAsTooLarge(got) || !strings.Contains(got.body, "limit of 1 GB") {
		t.Fatalf("default: not refused as too large:\n%.400s", got.body)
	}
	g.setSetting("save_upload_mb", 2048)
	g.setSetting("mod_upload_mb", 300)
	for _, c := range []struct {
		name, path, field string
		over              int64
	}{
		{"save", worldPath(id) + "/saves", "save", 1<<30 + 1},
		{"import", PathImportUpload, "save", 1<<30 + 1},
		{"mod", worldPath(id) + "/mods/upload", "mod", 200<<20 + 1},
	} {
		if got := g.postDeclared(c.path, c.field, c.over); uploadRefusedAsTooLarge(got) {
			t.Errorf("%s at %d bytes is still refused as too large:\n%.400s", c.name, c.over, got.body)
		}
	}
	// The new figure is the ceiling now.
	if got := g.postDeclared(worldPath(id)+"/saves", "save", 2<<30+1); !strings.Contains(got.body, "limit of 2 GB") {
		t.Errorf("over the raised limit, the refusal does not say 2 GB:\n%.400s", got.body)
	}
}

// DESIGN 7.10: each page's drop zone says the current limit, in the hint and
// in the script's message.
func TestDropZonesShowTheUploadSettings(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base", "alpha_1.0.0")
	g.setSetting("save_upload_mb", 500)
	g.setSetting("mod_upload_mb", 50)
	wantIn(t, "saves tab", g.get(worldPath(id)+"/saves"), `data-limit="500 MB"`, "500 MB")
	wantIn(t, "import page", g.get(PathImport), `data-limit="500 MB"`)
	_, mods := g.getStatus(modsPath(id))
	wantIn(t, "mods tab", mods, `data-limit="50 MB"`, `data-limit="4 MB"`)
	if strings.Contains(mods, `data-limit="200 MB"`) {
		t.Error("the mods tab still says the default limit")
	}
}

// An unresolved mod's upload field in a plan says the mod limit too.
func TestPlanUploadZoneShowsTheModSetting(t *testing.T) {
	hn := newHarness(t, nil)
	hn.setSetting("mod_upload_mb", 64)
	s := hn.generalServer()
	if got := s.modUploadLimit(context.Background()); got != 64<<20 {
		t.Fatalf("limit %d", got)
	}
	z := ModPlanPage{UploadLimit: zoneLimit(s.modUploadLimit(context.Background()))}.UploadZone("alpha", "1.0.0")
	if got := english(z.Limit()); got != "64 MB" {
		t.Errorf("the zone says %q, want 64 MB", got)
	}
}

// SPEC 14.6: the body limit is asked on every request, so a change applies
// to the next upload without a restart.
func TestUploadRouteReadsItsLimitPerRequest(t *testing.T) {
	hn := newHarness(t, nil)
	cookie := hn.signIn()
	s := &server{
		cfg: hn.cfg, store: hn.store, log: hn.slog, now: hn.clock.Now,
		sessions:     auth.NewSessionManager(hn.store, hn.cfg.SessionIdle, hn.cfg.SecureCookies(), hn.clock.Now),
		mux:          http.NewServeMux(),
		uploadLimits: map[string]limitFunc{},
	}
	limit := int64(10)
	s.handleUpload("POST /up", func(context.Context) int64 { return limit }, func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		}
	})
	h := s.limitBody(s.mux)
	send := func(size int) int {
		rec := &deadlineSpy{ResponseRecorder: httptest.NewRecorder()}
		r := httptest.NewRequest(http.MethodPost, "/up", strings.NewReader(strings.Repeat("x", size)))
		r.RemoteAddr = peerUntrusted
		r.AddCookie(cookie)
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	for _, c := range []struct {
		limit int64
		size  int
		want  int
	}{{10, 11, http.StatusRequestEntityTooLarge}, {10, 10, http.StatusOK}, {20, 15, http.StatusOK}, {5, 6, http.StatusRequestEntityTooLarge}} {
		limit = c.limit
		if got := send(c.size); got != c.want {
			t.Errorf("limit %d, %d bytes: %d, want %d", c.limit, c.size, got, c.want)
		}
	}
}

// recordingPortal remembers the byte limit a download was given.
type recordingPortal struct {
	ModPortal
	maxBytes int64
}

func (p *recordingPortal) Download(_ context.Context, _, _ string, _ modportal.Release, _ *os.File, maxBytes int64) error {
	p.maxBytes = maxBytes
	return nil
}

// SPEC 14.10: a mod downloaded from the portal is capped at the mod upload
// limit, read when the download starts.
func TestPortalDownloadFollowsTheModUploadLimit(t *testing.T) {
	hn := newHarness(t, nil)
	acct, err := factorioaccount.New(factorioaccount.Account{Username: secret.New("u"), Token: secret.New("t")}, secret.Secret{}, hn.store)
	if err != nil {
		t.Fatal(err)
	}
	s := hn.generalServer()
	rec := &recordingPortal{}
	f := accountFetcher{account: acct, portal: rec, limit: s.modUploadLimit}
	for _, mb := range []int{200, 20, 700} {
		hn.setSetting("mod_upload_mb", mb)
		if err := f.Download(context.Background(), modportal.Release{}, nil); err != nil {
			t.Fatal(err)
		}
		if rec.maxBytes != int64(mb)<<20 {
			t.Errorf("setting %d MB: the download was capped at %d bytes", mb, rec.maxBytes)
		}
	}
}
