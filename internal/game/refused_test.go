package game

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// SPEC 12.9: a fault in a switch's checks, such as Docker failing, is not a
// refusal: it is audited as the switch itself, which then failed.
func TestSwitchCheckFaultIsNotARefusal(t *testing.T) {
	v := newInvRig(t, "Alpha", "Beta")
	a, b := v.ids[0], v.ids[1]
	v.start(a)
	before := len(v.audit.list())
	v.game.Daemon().SetFailure(500, "daemon down")
	err := v.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{}, true)
	v.game.Daemon().SetFailure(0, "")
	if err == nil || IsRefusal(err) {
		t.Fatalf("got %v, want a Docker fault", err)
	}
	added := v.audit.list()[before:]
	if len(added) != 1 || added[0].Action != AuditSwitch {
		t.Errorf("audit entries %+v, want one plain switch", added)
	}
}

// SPEC 12.9: an action made through an API token records the token's name in
// its audit entry.
func TestAuditEntryNamesTheAPIToken(t *testing.T) {
	r := newRig(t, rigOptions{})
	id, err := r.worlds.Create(context.Background(), "Empty", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	by := Actor{UserID: 7, Username: "alice", Token: "bot"}
	if err := r.c.Start(context.Background(), id, by, StartOptions{}); err == nil {
		t.Fatal("a world with no save was not refused")
	}
	entries := r.audit.list()
	if len(entries) == 0 {
		t.Fatal("no audit entry")
	}
	if e := entries[len(entries)-1]; e.TokenName != "bot" || e.Username != "alice" {
		t.Errorf("audit entry %+v, want token name bot and user alice", e)
	}
}

// SPEC 12.9: a start or restart the checks refuse is recorded as a refusal,
// with the reason, in one audit entry and one log line, and never as the
// action itself.
func TestRefusalIsAuditedAndLoggedWithReason(t *testing.T) {
	for _, action := range []string{AuditStart, AuditRestart} {
		t.Run(action, func(t *testing.T) {
			var logs lockedBuffer
			r := newRig(t, rigOptions{logs: &logs})
			id, err := r.worlds.Create(context.Background(), "Empty", "2.1.24", false)
			if err != nil {
				t.Fatal(err)
			}
			before := len(r.audit.list())
			switch action {
			case AuditStart:
				err = r.c.Start(context.Background(), id, admin, StartOptions{})
			default:
				err = r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{})
			}
			if err == nil {
				t.Fatal("a world with no save was not refused")
			}
			added := r.audit.list()[before:]
			if len(added) != 1 {
				t.Fatalf("audit entries %+v, want one", added)
			}
			if e := added[0]; e.Action != action+"-refused" || e.WorldID != id || e.Detail != err.Error() || e.Username != admin.Username {
				t.Errorf("audit entry %+v, want %s-refused with the reason %q", e, action, err)
			}
			if n := strings.Count(logs.String(), action+" refused"); n != 1 {
				t.Errorf("%d log lines name the refusal, want 1:\n%s", n, logs.String())
			}
			if !strings.Contains(logs.String(), "reason=") {
				t.Errorf("the log line does not give the reason:\n%s", logs.String())
			}
		})
	}
}

// SPEC 12.9: when a check fails and its audit entry cannot be written
// either, only the audit fault is returned, so the check's own error goes
// to the log rather than being lost.
func TestCheckFaultIsLoggedWhenItsAuditFails(t *testing.T) {
	var logs lockedBuffer
	r := newRig(t, rigOptions{logs: &logs})
	id := r.newWorld("Unaudited check")
	r.audit.setFail(true)
	err := r.c.checkFailed(context.Background(), admin, AuditStart, id, "", errors.New("the disk is on fire"))
	if err == nil || !strings.Contains(err.Error(), "the database is gone") {
		t.Fatalf("got %v, want the audit fault", err)
	}
	got := logs.String()
	if !strings.Contains(got, "a check before the action failed, and so did its audit entry") || !strings.Contains(got, "the disk is on fire") {
		t.Errorf("log %q, want the check's own error", got)
	}
}
