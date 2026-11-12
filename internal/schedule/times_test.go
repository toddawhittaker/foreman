package schedule

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func utc(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
}

func TestNextAfter(t *testing.T) {
	london, ny, lordHowe := zone(t, "Europe/London"), zone(t, "America/New_York"), zone(t, "Australia/Lord_Howe")
	for _, c := range []struct {
		name   string
		after  time.Time
		minute int
		loc    *time.Location
		want   time.Time
	}{
		{"later today", utc(2026, 11, 12, 3, 0), 4 * 60, london, utc(2026, 11, 12, 4, 0)},
		{"at the time itself is tomorrow", utc(2026, 11, 12, 4, 0), 4 * 60, london, utc(2026, 11, 13, 4, 0)},
		{"summer time", utc(2026, 7, 1, 0, 0), 4 * 60, london, utc(2026, 7, 1, 3, 0)},
		{"zone ahead of UTC, its tomorrow", utc(2026, 11, 12, 20, 0), 4 * 60, zone(t, "Asia/Tokyo"), utc(2026, 11, 12, 19, 0).AddDate(0, 0, 1)},
		{"midnight", utc(2026, 11, 12, 1, 0), 0, time.UTC, utc(2026, 11, 13, 0, 0)},
		{"23:59", utc(2026, 11, 12, 1, 0), 23*60 + 59, time.UTC, utc(2026, 11, 12, 23, 59)},
		// Clocks go from 02:00 to 03:00 on 8 March 2026 in New York: 02:30
		// does not exist, so the restart runs at 03:00 EDT, 07:00 UTC.
		{"gap runs at the next valid minute", utc(2026, 3, 8, 0, 0), 2*60 + 30, ny, utc(2026, 3, 8, 7, 0)},
		{"gap, at its start", utc(2026, 3, 8, 0, 0), 2 * 60, ny, utc(2026, 3, 8, 7, 0)},
		{"gap in London", utc(2026, 3, 29, 0, 0), 60 + 30, london, utc(2026, 3, 29, 1, 0)},
		// Lord Howe moves its clocks by half an hour: 02:00 to 02:30 local
		// on 4 October 2026, at 15:30 UTC the day before.
		{"half-hour gap", utc(2026, 10, 3, 12, 0), 2*60 + 15, lordHowe, utc(2026, 10, 3, 15, 30)},
		// Clocks go from 02:00 back to 01:00 on 1 November 2026 in New
		// York: 01:30 happens twice, and the first (EDT, 05:30 UTC) runs.
		{"repeated hour runs at the first", utc(2026, 11, 1, 0, 0), 60 + 30, ny, utc(2026, 11, 1, 5, 30)},
		{"repeated hour runs once", utc(2026, 11, 1, 5, 30), 60 + 30, ny, utc(2026, 11, 2, 6, 30)},
		{"after the first pass, not the second", utc(2026, 11, 1, 6, 0), 60 + 30, ny, utc(2026, 11, 2, 6, 30)},
	} {
		if got := nextAfter(c.after, c.minute, c.loc); !got.Equal(c.want) {
			t.Errorf("%s: nextAfter(%v) = %v, want %v", c.name, c.after, got.UTC(), c.want)
		}
	}
}

// A day with a repeated hour runs its restart once through the loop too.
func TestRepeatedHourRunsOnceThroughTheLoop(t *testing.T) {
	x := newRig(t, utc(2026, 11, 1, 0, 0), nightlyAt(60+30, "America/New_York"))
	x.g.setPlayers("alice")
	x.stepAt(utc(2026, 11, 1, 5, 30))
	x.stepAt(utc(2026, 11, 1, 5, 30).Add(5 * time.Minute))
	if x.s.State().Phase != PhaseWaiting {
		t.Fatal("did not wait at the first 01:30")
	}
	// Limit 0 would skip; this one waits an hour, to 06:30 UTC, which is
	// the second 01:30. That time must not begin a second wait.
	x.stepAt(utc(2026, 11, 1, 6, 30))
	if st := x.s.State(); st.Phase != PhaseSkipped || !st.Next.Equal(utc(2026, 11, 2, 6, 30)) {
		t.Fatalf("state = %+v, want skipped with tomorrow next", st)
	}
	x.stepAt(utc(2026, 11, 1, 6, 31))
	if x.s.State().Phase != PhaseSkipped || len(x.emits.all()) != 1 {
		t.Error("the repeated hour ran the schedule twice")
	}
}

func TestReadZones(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tzdata.zi")
	data := "# version 2026a\nZ Europe/London 0 - LMT\nZ Etc/UTC 0 - UTC\nZ America/New_York -4:56:2 - LMT\nL Europe/Kyiv Europe/Kiev\nL America/New_York US/Eastern\nR x 1 2 - - 0 -\n"
	if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{"UTC", "America/New_York", "Europe/Kiev", "Europe/London"}
	if got := readZones(file); !slices.Equal(got, want) {
		t.Errorf("zones = %v, want %v", got, want)
	}
	if got := readZones(filepath.Join(t.TempDir(), "missing")); !slices.Equal(got, []string{UTC}) {
		t.Errorf("without the file: %v, want only UTC", got)
	}
}

func TestValidZone(t *testing.T) {
	if _, err := os.Stat(zoneFile); err != nil {
		t.Skip("this system has no ", zoneFile)
	}
	for _, ok := range []string{"UTC", "Europe/London", "America/Argentina/Buenos_Aires", "Europe/Kiev"} {
		if !ValidZone(ok) {
			t.Errorf("%s refused", ok)
		}
	}
	for _, bad := range []string{"", "Local", "US/Eastern", "Etc/GMT+5", "Europe/../London", "Mars/Olympus"} {
		if ValidZone(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
