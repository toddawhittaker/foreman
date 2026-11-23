package web

import (
	"context"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// Fail safe: a retention that cannot be used at start, because the stored
// value is out of range or the read fails, never makes a world drop
// snapshots. Every world keeps up to the largest number.
func TestApplyRetentionFailsSafe(t *testing.T) {
	for name, breakIt := range map[string]func(t *testing.T, hn *harness){
		"a stored value out of range": func(t *testing.T, hn *harness) {
			if err := hn.store.SetAppSetting(context.Background(), settingSnapshotRetention.Name, "150", "alice", hn.clock.Now()); err != nil {
				t.Fatal(err)
			}
		},
		"a read error": func(t *testing.T, hn *harness) {
			if err := hn.store.Close(); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			hn := newHarness(t, nil)
			id := hn.seedSnapshotWorld()
			hn.worlds.SetKeepAutomatic(50)
			for range 14 {
				hn.clock.Advance(time.Minute)
				if _, err := hn.worlds.AutoSnapshot(id, world.ReasonBeforeRestore, world.LabelBeforeRestore, "alice"); err != nil {
					t.Fatal(err)
				}
			}
			breakIt(t, hn)
			hn.generalServer().applyRetention(context.Background())
			if got := hn.worlds.KeepAutomatic(); got != settingSnapshotRetention.Max {
				t.Fatalf("the manager keeps %d, want %d", got, settingSnapshotRetention.Max)
			}
			hn.clock.Advance(time.Minute)
			if _, err := hn.worlds.AutoSnapshot(id, world.ReasonBeforeRestore, world.LabelBeforeRestore, "alice"); err != nil {
				t.Fatal(err)
			}
			if list, _ := hn.worlds.Snapshots(id); len(list) != 15 {
				t.Errorf("%d snapshots left, want 15: nothing may be pruned", len(list))
			}
		})
	}
}

// A save sets the manager from the value it wrote, not from a read, so a
// failing read afterwards cannot change it.
func TestSaveGeneralAppliesWrittenRetention(t *testing.T) {
	hn := newHarness(t, nil)
	hn.worlds.SetKeepAutomatic(50)
	s := hn.generalServer()
	if err := s.saveGeneral(context.Background(), actor(hn.addUser("bob", testPassword)), map[string]int{settingSnapshotRetention.Key: 7}, nil); err != nil {
		t.Fatal(err)
	}
	if got := hn.worlds.KeepAutomatic(); got != 7 {
		t.Errorf("the manager keeps %d, want 7", got)
	}
}

func TestGeneralFieldPanicsOnUnknownKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("an unknown key did not panic")
		}
	}()
	(&GeneralPage{}).field("nope")
}

// SPEC 13.1: the switches are saved with the numbers, each audited once.
func TestSaveGeneralStoresSwitchesWithNumbers(t *testing.T) {
	hn := newHarness(t, nil)
	s, ctx := hn.generalServer(), context.Background()
	by := actor(hn.addUser("bob", testPassword))
	if err := s.saveGeneral(ctx, by, map[string]int{settingStopCountdown.Key: 5},
		map[string]bool{settingUpdateCheck.Key: false, settingWikiIcons.Key: true}); err != nil {
		t.Fatal(err)
	}
	if s.stopCountdown(ctx) != 5*time.Second || s.readBool(ctx, settingUpdateCheck) || !s.readBool(ctx, settingWikiIcons) {
		t.Error("a number or switch was not stored")
	}
	if len(hn.audits(AuditWikiIconsSet)) != 1 || len(hn.audits(AuditUpdateCheckSet)) != 1 {
		t.Error("each changed switch is audited once")
	}
	if err := s.saveGeneral(ctx, by, map[string]int{settingStopCountdown.Key: 99999}, map[string]bool{settingWikiIcons.Key: false}); err == nil {
		t.Fatal("an out of range value was accepted")
	}
	if !s.readBool(ctx, settingWikiIcons) {
		t.Error("a refused save still changed a switch")
	}
}
