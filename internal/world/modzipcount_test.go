package world

import (
	"archive/zip"
	"bytes"
	"strconv"
	"testing"
)

// manyEntries is a zip of a valid info.json and n empty stored files.
func manyEntries(t *testing.T, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("m_1.0.0/info.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`{"name":"m","version":"1.0.0"}`))
	for i := range n {
		if _, err := zw.CreateRaw(&zip.FileHeader{Name: strconv.Itoa(i), Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// SPEC 14.6: a crafted zip of many tiny entries is refused before
// archive/zip builds an entry for each, since the Mods page reads every zip
// on each visit.
func TestReadZipInfoRefusesManyEntriesWithoutBuildingThem(t *testing.T) {
	b := manyEntries(t, 50000)
	r := bytes.NewReader(b)
	allocs := testing.AllocsPerRun(1, func() {
		if _, ok := readZipInfo(r, int64(len(b))); ok {
			t.Error("a zip of 50000 entries was read")
		}
	})
	if allocs > 100 {
		t.Errorf("%v allocations to refuse it; the directory was built", allocs)
	}
}

// Over 65535 entries the count is in a zip64 record; it is read there.
func TestZipEntriesOverReadsZip64(t *testing.T) {
	b := manyEntries(t, 70000)
	if !zipEntriesOver(bytes.NewReader(b), int64(len(b)), maxModZipEntries) {
		t.Error("a zip64 zip of 70000 entries is not over the limit")
	}
}

// A zip whose end record understates the count is still counted by its
// headers, as archive/zip reads them.
func TestZipEntriesOverCountsHeadersNotTheEndRecord(t *testing.T) {
	b := manyEntries(t, maxModZipEntries+10)
	end := bytes.LastIndex(b, []byte("PK\x05\x06"))
	b[end+8], b[end+9], b[end+10], b[end+11] = 1, 0, 1, 0 // one entry, says the record
	if !zipEntriesOver(bytes.NewReader(b), int64(len(b)), maxModZipEntries) {
		t.Error("the end record's count was trusted")
	}
}

// A real mod zip is well under the limit and still reads.
func TestZipEntriesOverPassesAModZip(t *testing.T) {
	for _, n := range []int{0, 10, maxModZipEntries - 1} {
		b := manyEntries(t, n)
		if zipEntriesOver(bytes.NewReader(b), int64(len(b)), maxModZipEntries) {
			t.Errorf("%d entries counted as over", n+1)
		}
		if info, ok := readZipInfo(bytes.NewReader(b), int64(len(b))); !ok || info.Name != "m" {
			t.Errorf("%d entries: info %+v, %v", n+1, info, ok)
		}
	}
	if zipEntriesOver(bytes.NewReader([]byte("not a zip")), 9, maxModZipEntries) {
		t.Error("a file that is not a zip counted as over")
	}
}
