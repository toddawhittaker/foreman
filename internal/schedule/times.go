package schedule

import (
	"bufio"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// nextAfter is the first scheduled time strictly after t: minute minutes
// after midnight in loc, on t's day or a later one.
func nextAfter(t time.Time, minute int, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	for i := 0; ; i++ {
		if at := occurrence(y, m, d+i, minute, loc); at.After(t) {
			return at
		}
	}
}

// occurrence is the first instant on the given day whose wall clock in loc
// reaches minute. A time that a daylight-saving gap skips gives the first
// minute after the gap; a time the clocks pass twice gives the first pass,
// so a repeated hour runs once (SPEC 7.12).
func occurrence(y int, m time.Month, d, minute int, loc *time.Location) time.Time {
	y, m, d = time.Date(y, m, d, 12, 0, 0, 0, loc).Date()
	target := wallKey(y, m, d, minute)
	// time.Date picks either side of a gap or overlap, never more than the
	// size of the clock change away, so three hours before it is early enough.
	u := time.Date(y, m, d, minute/60, minute%60, 0, 0, loc).Add(-3 * time.Hour).Truncate(time.Minute)
	for range 12 * 60 {
		lt := u.In(loc)
		ly, lm, ld := lt.Date()
		if wallKey(ly, lm, ld, lt.Hour()*60+lt.Minute()) >= target {
			return u
		}
		u = u.Add(time.Minute)
	}
	return u
}

// wallKey orders wall-clock minutes: whole minutes since 1970-01-01 00:00 as
// if every day had 1440 of them.
func wallKey(y int, m time.Month, d, minute int) int64 {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()/60 + int64(minute)
}

// zoneFile lists every IANA zone and link in one file. The distroless image
// Foreman ships in has it, so time.LoadLocation and this list agree.
const zoneFile = "/usr/share/zoneinfo/tzdata.zi"

// regions are the first parts of the zone names offered; the rest are old
// or administrative names such as "US/Eastern" and "Etc/GMT+5".
var regions = []string{"Africa", "America", "Antarctica", "Arctic", "Asia", "Atlantic", "Australia", "Europe", "Indian", "Pacific"}

// UTC is offered first and is the zone of a new schedule.
const UTC = "UTC"

// Zones lists the time zones a schedule may use, sorted, UTC first. Links
// are included, so an older name a browser reports is still found.
var Zones = sync.OnceValue(func() []string { return readZones(zoneFile) })

func readZones(file string) []string {
	out := []string{UTC}
	f, err := os.Open(file) //nolint:gosec // the fixed zone file, or a test's
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		var name string
		switch {
		case len(fields) >= 2 && fields[0] == "Z":
			name = fields[1]
		case len(fields) >= 3 && fields[0] == "L":
			name = fields[2]
		default:
			continue
		}
		region, _, ok := strings.Cut(name, "/")
		if ok && slices.Contains(regions, region) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return append(out, slices.Compact(names)...)
}

// ValidZone reports whether a schedule may use the zone.
func ValidZone(name string) bool {
	if !slices.Contains(Zones(), name) {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}
