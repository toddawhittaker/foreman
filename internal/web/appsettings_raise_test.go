package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// SPEC 17.5: the confirm step is decided by the real trim count, so a raise
// that still removes snapshots asks first. Here retention was lowered to 3
// while six automatic snapshots remained (each world drops extras only at its
// next automatic snapshot), then raised to 5, which still removes one.
func TestGeneralRaiseThatStillTrimsAsksFirst(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.seedAutos(6)
	if err := hn.store.SetAppSetting(context.Background(), settingSnapshotRetention.Name, "3", "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	rec := hn.post(generalPath(), generalForm(map[string]string{"snapshot_retention": "5"}), withCookie(c))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "1 automatic snapshot will be removed") {
		t.Fatalf("a raise that trims: %d\n%s", rec.Code, rec.Body.String())
	}
	if got := hn.generalServer().snapshotRetention(context.Background()); got != 3 {
		t.Errorf("asking saved retention %d, want 3", got)
	}
	hn.wantRedirect(hn.post(generalPath(), confirmedForm(map[string]string{"snapshot_retention": "5"}, 1, 1), withCookie(c)), generalPath()+"?done=saved")
	if got := hn.generalServer().snapshotRetention(context.Background()); got != 5 {
		t.Errorf("the confirm saved retention %d, want 5", got)
	}
}
