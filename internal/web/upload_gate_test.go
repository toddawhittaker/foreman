package web

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	msgNoLength = "Your browser did not say how big the file is. Try again from the upload form."
	msgTooMany  = "Two uploads are already running. Wait for one to finish and try again."
)

// answer reads the response on conn, waiting at most wait. ok is false when
// none came, which means the server is still waiting for more of the body.
func answer(conn net.Conn, wait time.Duration) (status int, body string, ok bool) {
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return 0, "", false
	}
	defer func() { _ = resp.Body.Close() }()
	var b bytes.Buffer
	_, _ = b.ReadFrom(resp.Body)
	return resp.StatusCode, b.String(), true
}

// failingReader fails the test if anything reads it.
type failingReader struct{ t *testing.T }

func (f failingReader) Read([]byte) (int, error) {
	f.t.Helper()
	f.t.Error("the handler read the body")
	return 0, io.EOF
}

// SPEC 14.6: an upload that does not say how big it is would skip the
// size limit and the space check, so it is refused before its body is read.
func TestUploadWithoutContentLengthIsRefusedBeforeTheBodyIsRead(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	for _, path := range []string{"/worlds/" + id + "/saves", PathImportUpload} {
		r := httptest.NewRequest(http.MethodPost, path, failingReader{t})
		r.ContentLength = -1
		r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
		r.AddCookie(c)
		rec := hn.serveLong(r)
		if rec.Code != http.StatusLengthRequired {
			t.Errorf("%s: status %d, want 411", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), msgNoLength) {
			t.Errorf("%s: the page does not say what to do:\n%s", path, rec.Body)
		}
	}
}

// The same over a real connection with a chunked body, which is how a
// client leaves out the length. Nothing is written.
func TestChunkedUploadIsRefusedOverTheWire(t *testing.T) {
	sc := newSecCase(t)
	before := sc.tree(t)
	u, _ := url.Parse(sc.ls.url)
	for _, path := range []string{"/worlds/" + sc.id + "/saves", PathImportUpload} {
		conn, err := net.Dial("tcp", u.Host)
		if err != nil {
			t.Fatal(err)
		}
		head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\nContent-Type: multipart/form-data; boundary=x\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n0\r\n\r\n",
			path, u.Host, sc.cookie.Get("Cookie"))
		if _, err := conn.Write([]byte(head)); err != nil {
			t.Fatal(err)
		}
		status, body, ok := answer(conn, 5*time.Second)
		_ = conn.Close()
		if !ok || status != http.StatusLengthRequired || !strings.Contains(body, msgNoLength) {
			t.Errorf("%s: status %d (answered %v), want 411 with the message:\n%s", path, status, ok, body)
		}
	}
	sameData(t, "after refused uploads", sc.tree(t), before)
}

// SPEC 14.6: at most two uploads run at once; a third is told to wait,
// without its body being read, and a finished or refused upload gives its
// place back.
func TestAtMostTwoUploadsRunAtOnce(t *testing.T) {
	shortIdle(t, time.Minute)
	sc := newSecCase(t)
	path := "/worlds/" + sc.id + "/saves"

	// Every way out of the handler gives the place back: refusals of
	// several kinds, a missing world, then a success.
	for _, bad := range []string{"not a zip", ""} {
		if got := sc.upload(t, path, nil, "bad.zip", []byte(bad)); got.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("bad upload: status %d", got.StatusCode)
		}
	}
	if got := sc.upload(t, "/worlds/nope/saves", nil, "a.zip", saveZip(t)); got.StatusCode != http.StatusNotFound {
		t.Fatalf("missing world: status %d", got.StatusCode)
	}
	if got := sc.upload(t, PathImportUpload, hostileImportFields, "ok.zip", saveZip(t)); got.StatusCode != http.StatusSeeOther {
		t.Fatalf("good import: status %d\n%s", got.StatusCode, got.body)
	}

	bigBody := bytes.Repeat([]byte("z"), 512<<10)

	// Two uploads in progress: headers and a first part of the body sent.
	// The server starts a connection's handler when it gets to it, so a
	// probe sent at once can take a place before the second upload has its
	// own. A taken-over upload is answered 429; then the round is thrown
	// away and begun again, so the round that counts has both uploads
	// holding a place when the third arrives.
	var held [2]net.Conn
	var rest [2][]byte
	var status int
	var body string
	settled := false
	for round := 0; round < 10 && !settled; round++ {
		for i := range held {
			held[i], rest[i] = sc.slowUpload(t, path, fmt.Sprintf("held%d-%d.zip", round, i), saveZip(t))
			if _, err := held[i].Write(rest[i][:10]); err != nil {
				t.Fatal(err)
			}
			rest[i] = rest[i][10:]
		}
		// The third declares more than the server would drain and sends
		// headers only. It gets its answer anyway, so its body was not read.
		status, body = 0, ""
		for range 20 {
			third, _ := sc.slowUpload(t, path, "third.zip", bigBody)
			var ok bool
			status, body, ok = answer(third, 200*time.Millisecond)
			_ = third.Close()
			if ok {
				break
			}
		}
		settled = true
		for _, conn := range held {
			if _, _, answered := answer(conn, 100*time.Millisecond); answered {
				settled = false
			}
		}
		if !settled {
			for _, conn := range held {
				_ = conn.Close()
			}
			time.Sleep(300 * time.Millisecond) // let the places come back
		}
	}
	if !settled {
		t.Fatal("the two uploads never both held a place")
	}
	if status != http.StatusTooManyRequests || !strings.Contains(body, msgTooMany) {
		t.Fatalf("third upload: status %d, want 429 with the message:\n%s", status, body)
	}
	// The import route shares the same two places.
	imp, _ := sc.slowUpload(t, PathImportUpload, "fourth.zip", bigBody)
	if status, _, ok := answer(imp, 3*time.Second); !ok || status != http.StatusTooManyRequests {
		t.Errorf("import upload while two run: status %d (answered %v), want 429", status, ok)
	}
	_ = imp.Close()

	// The two in progress were not disturbed, and finish.
	for i, conn := range held {
		if status, _, ok := answer(conn, 100*time.Millisecond); ok {
			t.Fatalf("upload %d was answered early with %d", i, status)
		}
		if _, err := conn.Write(rest[i]); err != nil {
			t.Fatal(err)
		}
		if status, body, ok := answer(conn, 10*time.Second); !ok || status != http.StatusSeeOther {
			t.Errorf("upload %d: status %d (answered %v), want 303\n%s", i, status, ok, body)
		}
	}
	// Both places are free again.
	for range 3 {
		if got := sc.upload(t, path, nil, fmt.Sprintf("after%d.zip", time.Now().UnixNano()), saveZip(t)); got.StatusCode != http.StatusSeeOther {
			t.Errorf("upload after the two finished: status %d\n%s", got.StatusCode, got.body)
		}
	}
}
