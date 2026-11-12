package web

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// shortIdle shortens the idle limit for one test.
func shortIdle(t *testing.T, d time.Duration) {
	t.Helper()
	old := idleLimit
	idleLimit = d
	t.Cleanup(func() { idleLimit = old })
}

// slowUpload opens a raw connection and sends a save upload's headers and
// the start of its body, then hands the connection back so the test can
// pace the rest. rest is the body still to send.
func (sc *secCase) slowUpload(t *testing.T, path, filename string, content []byte) (conn net.Conn, rest []byte) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("name", "Slow")
	_ = mw.WriteField("game_version", "2.0.72")
	fw, _ := mw.CreateFormFile("save", filename)
	_, _ = fw.Write(content)
	_ = mw.Close()
	u, _ := url.Parse(sc.ls.url)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n",
		path, u.Host, sc.cookie.Get("Cookie"), mw.FormDataContentType(), b.Len())
	if _, err := conn.Write([]byte(head)); err != nil {
		t.Fatal(err)
	}
	return conn, b.Bytes()
}

// SPEC 14.6: a long upload that keeps moving runs past the idle limit as
// many times over as it needs.
func TestSlowUploadThatKeepsMovingSucceeds(t *testing.T) {
	shortIdle(t, 300*time.Millisecond)
	sc := newSecCase(t)
	for _, path := range []string{"/worlds/" + sc.id + "/saves", PathImportUpload} {
		conn, rest := sc.slowUpload(t, path, "slow.zip", saveZip(t))
		// Ten pieces, each well inside the limit, the whole well past it.
		step := len(rest)/10 + 1
		for len(rest) > 0 {
			n := min(step, len(rest))
			if _, err := conn.Write(rest[:n]); err != nil {
				t.Fatalf("%s: write: %v", path, err)
			}
			rest = rest[n:]
			time.Sleep(150 * time.Millisecond)
		}
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther {
			t.Errorf("%s: status %d, want 303", path, resp.StatusCode)
		}
	}
}

// SPEC 14.6: an upload that stops sending ends once the idle limit passes,
// and leaves nothing behind.
func TestStalledUploadEnds(t *testing.T) {
	shortIdle(t, 300*time.Millisecond)
	sc := newSecCase(t)
	before := sc.tree(t)
	for _, path := range []string{"/worlds/" + sc.id + "/saves", PathImportUpload} {
		conn, rest := sc.slowUpload(t, path, "stalled.zip", bytes.Repeat([]byte("z"), 64<<10))
		if _, err := conn.Write(rest[:len(rest)/2]); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		// The server either answers with a refusal or drops the
		// connection; either way it stops waiting.
		_, _ = io.ReadAll(conn)
		if waited := time.Since(start); waited > 5*time.Second {
			t.Errorf("%s: the server waited %v for a stalled client", path, waited)
		}
	}
	// The handler removes its temp file once the read fails.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(sc.tree(t)) != len(before) {
		time.Sleep(50 * time.Millisecond)
	}
	sameData(t, "after stalled uploads", sc.tree(t), before)
}

// SPEC 6.11: both upload routes hold a reserve for the declared length
// for the whole upload, so the space they need is counted against other
// writes in flight, and an upload that does not fit is refused before any
// byte is written.
func TestUploadsHoldTheSpaceReserve(t *testing.T) {
	sc := newSecCase(t)
	t.Cleanup(world.SetSpaceReserveForTest(0))
	// A fixed reading, so other processes using the disk cannot change
	// what fits (SPEC 6.11).
	const declared = 64 << 20
	const free = 2 * declared
	t.Cleanup(world.SetFreeBytesForTest(free))
	before := sc.tree(t)
	for _, path := range []string{"/worlds/" + sc.id + "/saves", PathImportUpload} {
		// While a 64 MB upload is in flight, the same space is promised.
		conn, rest := sc.slowUpload(t, path, "held.zip", bytes.Repeat([]byte("z"), declared))
		if _, err := conn.Write(rest[:1024]); err != nil {
			t.Fatal(err)
		}
		held := false
		for range 100 {
			if world.CheckSpace(sc.data, free-declared/2) != nil {
				held = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !held {
			t.Errorf("%s: no reserve was held while the upload ran", path)
		}
		_ = conn.Close()

		// An upload larger than what is left is refused up front. The
		// closed upload above gives its reserve back once it notices.
		var release func()
		for range 100 {
			var err error
			if release, err = world.Reserve(sc.data, free-(1<<20)); err == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if release == nil {
			t.Fatal("the closed upload never gave its reserve back")
		}
		got := sc.upload(t, path, hostileImportFields, "big.zip", bytes.Repeat([]byte("z"), 2<<20))
		release()
		if got.StatusCode != http.StatusInsufficientStorage || !strings.Contains(got.body, "Nothing was changed") {
			t.Errorf("%s: status %d, want 507 in words", path, got.StatusCode)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(sc.tree(t)) != len(before) {
		time.Sleep(50 * time.Millisecond)
	}
	sameData(t, "after refused uploads", sc.tree(t), before)
}
