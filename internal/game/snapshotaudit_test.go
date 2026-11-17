package game

import (
	"context"
	"testing"
)

// SPEC 12.9: a manual snapshot is recorded, and one made through an API
// token carries the token's name.
func TestSnapshotIsAudited(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Audited")
	by := Actor{UserID: 7, Username: "alice", Token: "bot"}
	if _, err := r.c.Snapshot(context.Background(), id, by, "before the rocket"); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range r.audit.list() {
		if e.Action != AuditSnapshot {
			continue
		}
		got = append(got, e.Username+"|"+e.TokenName+"|"+e.WorldID+"|"+e.Detail)
	}
	if want := "alice|bot|" + id + "|before the rocket"; len(got) != 1 || got[0] != want {
		t.Errorf("snapshot audit entries = %q, want [%q]", got, want)
	}
}

// SPEC 12.9: when the audit entry cannot be written, no snapshot is taken.
func TestSnapshotIsNotTakenWhenTheAuditFails(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("No audit")
	r.audit.setFail(true)
	if _, err := r.c.Snapshot(context.Background(), id, admin, "x"); err == nil {
		t.Fatal("Snapshot succeeded without an audit entry")
	}
	if snaps, err := r.worlds.Snapshots(id); err != nil || len(snaps) != 0 {
		t.Errorf("snapshots = %v, %v; want none", snaps, err)
	}
}

// SPEC 12.9: a restore is recorded with the snapshot it restores and the
// token's name, and the automatic "Before restore" snapshot adds no
// `snapshot` entry of its own.
func TestRestoreIsAudited(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Restored")
	snap, err := r.c.Snapshot(context.Background(), id, admin, "good")
	if err != nil {
		t.Fatal(err)
	}
	by := Actor{UserID: 7, Username: "alice", Token: "bot"}
	if _, err := r.c.Restore(context.Background(), id, by, snap.Name, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	var restores []string
	snapshots := 0
	for _, e := range r.audit.list() {
		switch e.Action {
		case AuditRestore:
			restores = append(restores, e.Username+"|"+e.TokenName+"|"+e.WorldID+"|"+e.Detail)
		case AuditSnapshot:
			snapshots++
		}
	}
	if want := "alice|bot|" + id + "|" + snap.Name + " (good)"; len(restores) != 1 || restores[0] != want {
		t.Errorf("restore audit entries = %q, want [%q]", restores, want)
	}
	if snapshots != 1 {
		t.Errorf("%d snapshot entries, want only the manual one", snapshots)
	}
}

// SPEC 12.9: when the audit entry cannot be written, nothing is restored.
func TestRestoreIsNotDoneWhenTheAuditFails(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Not restored")
	snap, err := r.c.Snapshot(context.Background(), id, admin, "good")
	if err != nil {
		t.Fatal(err)
	}
	r.audit.setFail(true)
	if _, err := r.c.Restore(context.Background(), id, admin, snap.Name, StopOptions{}); err == nil {
		t.Fatal("Restore succeeded without an audit entry")
	}
	if snaps, err := r.worlds.Snapshots(id); err != nil || len(snaps) != 1 {
		t.Errorf("snapshots = %d, %v; want only the original, so no Before restore", len(snaps), err)
	}
}

// A snapshot that does not exist is refused before anything is recorded.
func TestRestoreOfAMissingSnapshotIsNotAudited(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Missing")
	if _, err := r.c.Restore(context.Background(), id, admin, "20200101-000000", StopOptions{}); err == nil {
		t.Fatal("Restore of a missing snapshot succeeded")
	}
	for _, e := range r.audit.list() {
		if e.Action == AuditRestore {
			t.Errorf("audited a refused restore: %+v", e)
		}
	}
}
