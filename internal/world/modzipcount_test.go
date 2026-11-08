package world

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
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

// zipEnd is where the end of central directory record starts in b.
func zipEnd(b []byte) int {
	return bytes.LastIndex(b, []byte("PK\x05\x06"))
}

// archive/zip finds an end record anywhere in the last 65 KiB, with junk
// after it, so the count must find it there too.
func TestZipEntriesOverFindsTheEndRecordBeforeTrailingJunk(t *testing.T) {
	b := append(manyEntries(t, maxModZipEntries+10), make([]byte, 66000)...)
	if !zipEntriesOver(bytes.NewReader(b), int64(len(b)), maxModZipEntries) {
		t.Error("an end record 66000 bytes before the end was not found")
	}
}

// archive/zip starts reading at the end record less the directory size,
// even when that makes its base offset negative.
func TestZipEntriesOverCountsFromANegativeBase(t *testing.T) {
	b := manyEntries(t, maxModZipEntries+10)
	e := zipEnd(b)
	start := int(binary.LittleEndian.Uint32(b[e+16:]))
	binary.LittleEndian.PutUint16(b[e+8:], 0)
	binary.LittleEndian.PutUint16(b[e+10:], 0)
	binary.LittleEndian.PutUint32(b[e+12:], uint32(e-start))  //nolint:gosec // small test values
	binary.LittleEndian.PutUint32(b[e+16:], uint32(len(b)-1)) //nolint:gosec // small test values
	if !zipEntriesOver(bytes.NewReader(b), int64(len(b)), maxModZipEntries) {
		t.Error("headers read from endAt-dirSize were not counted")
	}
}

// A directory size of 0xffff is not a zip64 trigger for archive/zip, so a
// fake zip64 record behind it must not be believed.
func TestZipEntriesOverIgnoresZip64WithoutGosTrigger(t *testing.T) {
	b := manyEntries(t, maxModZipEntries+10)
	e := zipEnd(b)
	start := binary.LittleEndian.Uint32(b[e+16:])
	head, end := append([]byte{}, b[:e]...), append([]byte{}, b[e:]...)
	z64 := make([]byte, 56)
	binary.LittleEndian.PutUint32(z64, 0x06064b50)
	binary.LittleEndian.PutUint64(z64[32:], 1)
	binary.LittleEndian.PutUint64(z64[48:], uint64(len(head)))
	loc := make([]byte, 20)
	binary.LittleEndian.PutUint32(loc, 0x07064b50)
	binary.LittleEndian.PutUint64(loc[8:], uint64(len(head)))
	binary.LittleEndian.PutUint32(loc[16:], 1)
	binary.LittleEndian.PutUint16(end[8:], 0)
	binary.LittleEndian.PutUint16(end[10:], 0)
	binary.LittleEndian.PutUint32(end[12:], 0xffff)
	binary.LittleEndian.PutUint32(end[16:], start)
	b = append(append(append(head, z64...), loc...), end...)
	if !zipEntriesOver(bytes.NewReader(b), int64(len(b)), maxModZipEntries) {
		t.Error("a fake zip64 record hid the headers archive/zip reads")
	}
}
