package web

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Gate tests for set_foreman_settings (SPEC 21.8) and the
// upload limits it sets (SPEC 14.6, 14.10) that
// appsettings_test.go and settingsreach_test.go leave out.

// SPEC 21.8, 14.19: a code stands for the exact changes it previewed. A
// confirm with other in-range values is refused and saves nothing, and the
// code is spent by that refusal, so the previewed values cannot follow.
func TestMCPSetForemanSettingsCodeIsForItsValues(t *testing.T) {
	r := newForemanRig(t, keyed)
	ctx := context.Background()
	args := map[string]any{"snapshot_retention": 2}
	code := askCode(t, r.cs, "set_foreman_settings", args)
	res, out := r.run(t, "set_foreman_settings", withCode(map[string]any{"snapshot_retention": 3}, code))
	if !res.IsError || out["done"] == true {
		t.Fatalf("other values with the code: %s %s", resultText(res), asJSON(out))
	}
	if got := r.generalServer().snapshotRetention(ctx); got != 10 {
		t.Fatalf("retention %d after a refused confirm", got)
	}
	if res, _ := r.run(t, "set_foreman_settings", withCode(args, code)); !res.IsError || resultText(res) != errConfirmRefused.Error() {
		t.Errorf("the code after a refusal: %s", resultText(res))
	}
	if got := r.generalServer().snapshotRetention(ctx); got != 10 || len(r.auditRows(AuditSnapshotRetentionSet)) != 0 {
		t.Errorf("retention %d, %d audit rows, after refusals", got, len(r.auditRows(AuditSnapshotRetentionSet)))
	}
}

// SPEC 21.8: a confirm refused because a value moved spends its
// code, so moving the value back does not make the old code good again;
// and when the moved value is the one being changed, the fresh preview
// starts from the new value.
func TestMCPSetForemanSettingsMovedSpendsTheCode(t *testing.T) {
	r := newForemanRig(t, keyed)
	ctx := context.Background()
	args := map[string]any{"stop_countdown_seconds": 30}
	code := askCode(t, r.cs, "set_foreman_settings", args)
	r.setSetting(settingStopCountdown.Name, 45)
	res, out := r.run(t, "set_foreman_settings", withCode(args, code))
	if !res.IsError || out["done"] != false || out["confirm_code"] == "" || out["confirm_code"] == code {
		t.Fatalf("moved: %s %s", resultText(res), asJSON(out))
	}
	if got := asJSON(out["changes"]); got != `[{"from":45,"setting":"stop_countdown_seconds","to":30}]` {
		t.Errorf("the fresh preview's changes %s", got)
	}
	r.setSetting(settingStopCountdown.Name, 60)
	if res, _ := r.run(t, "set_foreman_settings", withCode(args, code)); !res.IsError || resultText(res) != errConfirmRefused.Error() {
		t.Errorf("the old code after the value moved back: %s", resultText(res))
	}
	if got := r.generalServer().stopCountdown(ctx); got != 60*time.Second || len(r.auditRows(AuditStopCountdownSet)) != 0 {
		t.Errorf("countdown %s after refusals", got)
	}
}

// SPEC 13.1: each setting takes its whole range, ends
// included, and nothing outside it: one past either end, a negative, a
// fraction or a huge number is refused with no code.
func TestMCPSetForemanSettingsRangeEnds(t *testing.T) {
	r := newForemanRig(t, keyed)
	for _, d := range generalSettings {
		for _, n := range []int{d.Min, d.Max} {
			res, out := r.run(t, "set_foreman_settings", map[string]any{d.Key: n})
			if res.IsError || out["done"] != false {
				t.Errorf("%s=%d refused: %s", d.Key, n, resultText(res))
			}
		}
		for _, bad := range []any{d.Min - 1, d.Max + 1, -1, 2.5, 1 << 40} {
			res, err := r.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "set_foreman_settings", Arguments: map[string]any{d.Key: bad}})
			if err == nil && (!res.IsError || strings.Contains(asJSON(res.StructuredContent), confirmCodeKey)) {
				t.Errorf("%s=%v accepted: %s", d.Key, bad, resultText(res))
			}
		}
	}
	for _, d := range generalSettings {
		if got := r.generalServer().intSetting(context.Background(), d); got != d.Default {
			t.Errorf("%s is %d after previews and refusals, want %d", d.Key, got, d.Default)
		}
	}
}

// chunkedUpload posts a multipart upload whose file part is size zero bytes,
// sent chunked with no Content-Length, so only counting the bytes as they
// arrive can refuse it.
func chunkedUpload(g *gameRig, path, field string, size int64) reply {
	g.t.Helper()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		_ = mw.WriteField("name", "Imported")
		fw, err := mw.CreateFormFile(field, "big.zip")
		if err == nil {
			_, err = io.CopyN(fw, zeroReader{}, size)
		}
		if err == nil {
			err = mw.Close()
		}
		_ = pw.CloseWithError(err)
	}()
	req, err := http.NewRequest(http.MethodPost, g.url+path, pr)
	if err != nil {
		g.t.Fatal(err)
	}
	req.ContentLength = -1
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(g.cookie)
	resp, err := g.client.Do(req)
	if err != nil {
		// The server may close the connection once it has refused the body.
		return reply{}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	_ = pr.Close()
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, body: string(body)}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// SPEC 14.6: a limit lowered through set_foreman_settings holds
// for the very next upload of a save and of a mod. The server checks the
// size itself: an upload one byte over the new limit is refused as too
// large, and one sent chunked, with no size declared, is refused too, so
// the check cannot be dodged. Nothing is kept or audited.
func TestMCPLoweredUploadLimitHoldsForTheNextUpload(t *testing.T) {
	g, cs := modsMCP(t, true)
	id := g.world20("Base")
	confirmCall(t, cs, "set_foreman_settings", map[string]any{"save_upload_mb": 100, "mod_upload_mb": 10})
	savesDir := filepath.Join(g.worlds.Dirs().Worlds, id, world.SavesDir)
	saves, _ := os.ReadDir(savesDir)
	audits := len(g.audit())
	for _, c := range []struct {
		name, path, field, limit string
		size                     int64
	}{
		{"mod", worldPath(id) + "/mods/upload", "mod", "10 MB", 10<<20 + 1},
		{"save", worldPath(id) + "/saves", "save", "100 MB", 100<<20 + 1},
	} {
		if got := g.postDeclared(c.path, c.field, c.size); !strings.Contains(got.body, "larger than the limit of "+c.limit) {
			t.Errorf("%s of %d bytes: %d, want the size refusal naming %s\n%.400s", c.name, c.size, got.StatusCode, c.limit, got.body)
		}
		got := chunkedUpload(g.gameRig, c.path, c.field, c.size)
		if got.StatusCode != 0 && got.StatusCode != http.StatusLengthRequired && !uploadRefusedAsTooLarge(got) {
			t.Errorf("%s of %d bytes, chunked: %d, want a refusal for its size\n%.400s", c.name, c.size, got.StatusCode, got.body)
		}
	}
	if names := dirNames(t, g.modsDir(id)); len(names) != 0 {
		t.Errorf("the mods folder holds %v", names)
	}
	if after, _ := os.ReadDir(savesDir); len(after) != len(saves) {
		t.Errorf("the saves folder went from %d to %d files", len(saves), len(after))
	}
	if got := g.audit(); len(got) != audits {
		t.Errorf("a refused upload was audited: %+v", got[audits:])
	}
}

// limitSpy passes downloads through to the portal and remembers the byte
// limit each was given.
type limitSpy struct {
	ModPortal
	limits []int64
}

func (p *limitSpy) Download(ctx context.Context, user, token string, r modportal.Release, dst *os.File, maxBytes int64) error {
	p.limits = append(p.limits, maxBytes)
	return p.ModPortal.Download(ctx, user, token, r, dst, maxBytes)
}

// SPEC 14.10: a mod change downloads under the mod upload limit
// as it stands when the change runs, so a portal file over a lowered limit
// is refused and nothing is placed.
func TestPortalDownloadOverTheLoweredModLimitIsRefused(t *testing.T) {
	spy := &limitSpy{}
	g, cs := modsMCPWrapped(t, true, func(p ModPortal) ModPortal { spy.ModPortal = p; return spy })
	big := g.portal.AddZip("alpha", "1.0.0")
	g.portal.SetMode(big, modportaltest.Oversized)
	id := g.world20("Base")
	confirmCall(t, cs, "set_foreman_settings", map[string]any{"mod_upload_mb": 10})

	_, res, out := confirmCall(t, cs, "change_mods", map[string]any{"world_id": id, "kind": "install", "names": []string{"alpha"}})
	if res.IsError {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if err := g.waitResult(id, game.AuditMods); err == nil {
		t.Fatal("a download over the limit was installed")
	}
	if len(spy.limits) == 0 || spy.limits[0] != 10<<20 {
		t.Errorf("download limits %v, want 10 MB", spy.limits)
	}
	if g.hasFile(id, "alpha_1.0.0.zip") {
		t.Error("the oversized mod was placed")
	}
}
