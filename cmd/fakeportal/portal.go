package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
)

const (
	portalHost   = "mods.factorio.com"
	downloadHost = "dl-mod.factorio.com"
)

// Fake is the fake mod portal behind a test-only HTTPS proxy (SPEC 9.12; the
// product has no portal address setting). Foreman is started with
// HTTPS_PROXY set to Proxy and SSL_CERT_FILE set to CAFile, and its portal
// client then reaches this fake while believing it talks to factorio.com.
type Fake struct {
	Proxy   string // http://127.0.0.1:PORT, for HTTPS_PROXY
	CAFile  string // PEM of the test certificate authority, for SSL_CERT_FILE
	Control string // http://127.0.0.1:PORT, the test hooks

	portal *modportaltest.Portal
	leaf   tls.Certificate
	front  *httputil.ReverseProxy
	srvs   []*http.Server

	mu      sync.Mutex
	down    bool
	connect int
	refused []string
	reqs    []request
	served  map[string]modportaltest.Served // by name_version
	added   []string                        // mod names added, in order
}

type request struct {
	Host string `json:"host"`
	Path string `json:"path"` // never the query, which carries the download token
}

// Start runs the fake. The CA file is written into dir. t is used only by the
// recorded-fixture portal, for its cleanup and fatal errors.
func Start(t testing.TB, dir string) (*Fake, error) {
	f := &Fake{portal: modportaltest.New(t), served: map[string]modportaltest.Served{}}
	caPEM, leaf, err := makeCerts()
	if err != nil {
		return nil, err
	}
	f.leaf = leaf
	f.CAFile = filepath.Join(dir, "fakeportal-ca.pem")
	if err := os.WriteFile(f.CAFile, caPEM, 0o600); err != nil {
		return nil, err
	}
	target, err := url.Parse(f.portal.URL)
	if err != nil {
		return nil, err
	}
	f.front = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = target.Host
		},
		ModifyResponse: f.rewriteResponse,
	}

	if f.Proxy, err = f.listen(http.HandlerFunc(f.serveProxy)); err != nil {
		return nil, err
	}
	if f.Control, err = f.listen(f.controlHandler()); err != nil {
		return nil, err
	}
	return f, nil
}

// Close stops the listeners. The recorded-fixture portal closes with t.
func (f *Fake) Close() {
	for _, s := range f.srvs {
		_ = s.Close()
	}
}

func (f *Fake) listen(h http.Handler) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	f.srvs = append(f.srvs, s)
	go func() { _ = s.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}

// hostAllowed is the only door out of the proxy: the portal and the download
// hosts, on port 443. Everything else is refused, so a test can never reach
// the internet through it.
func hostAllowed(hostport string) bool {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil || port != "443" {
		return false
	}
	return host == portalHost || (strings.HasSuffix(host, ".factorio.com") && len(host) > len(".factorio.com"))
}

func (f *Fake) serveProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		http.Error(w, "the fake portal proxy takes CONNECT only", http.StatusForbidden)
		return
	}
	f.mu.Lock()
	f.connect++
	down := f.down
	f.mu.Unlock()
	if !hostAllowed(r.Host) {
		f.mu.Lock()
		f.refused = append(f.refused, r.Host)
		f.mu.Unlock()
		http.Error(w, "the fake portal proxy refuses "+r.Host, http.StatusForbidden)
		return
	}
	if down {
		http.Error(w, "the fake portal is down", http.StatusServiceUnavailable)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "cannot hijack", http.StatusInternalServerError)
		return
	}
	raw, _, err := hj.Hijack()
	if err != nil {
		return
	}
	_, _ = raw.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	conn := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{f.leaf}, MinVersion: tls.VersionTLS12})
	inner := &http.Server{Handler: http.HandlerFunc(f.servePortal), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = inner.Serve(newOneConn(conn)) }()
}

// oneConn is a listener that hands out a single connection, then reports
// itself closed when that connection ends, so the server on it returns.
type oneConn struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func newOneConn(c net.Conn) *oneConn {
	l := &oneConn{ch: make(chan net.Conn, 1), done: make(chan struct{})}
	l.ch <- &notifyConn{Conn: c, onClose: func() { l.once.Do(func() { close(l.done) }) }}
	return l
}

func (l *oneConn) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, errors.New("closed")
	}
}
func (l *oneConn) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *oneConn) Addr() net.Addr { return &net.TCPAddr{} }

type notifyConn struct {
	net.Conn
	onClose func()
}

func (c *notifyConn) Close() error { c.onClose(); return c.Conn.Close() }

// servePortal answers one request that came through a tunnel.
func (f *Fake) servePortal(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, request{Host: host, Path: r.URL.Path})
	f.mu.Unlock()

	if host != portalHost {
		// A download host serves files only; the portal's API is not there.
		if !strings.HasPrefix(r.URL.Path, "/cdn/") {
			http.NotFound(w, r)
			return
		}
		f.front.ServeHTTP(w, r)
		return
	}
	// The real portal sends a download with no credentials to its login page.
	if strings.HasPrefix(r.URL.Path, "/download/") {
		q := r.URL.Query()
		if q.Get("username") == "" || q.Get("token") == "" {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusFound)
			return
		}
	}
	f.front.ServeHTTP(w, r)
}

// rewriteResponse makes the fake look like the real portal: a download
// redirects to a download host on factorio.com, and the mod list includes
// the mods a test added.
func (f *Fake) rewriteResponse(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusFound && strings.HasPrefix(resp.Header.Get("Location"), "/cdn/"):
		resp.Header.Set("Location", "https://"+downloadHost+resp.Header.Get("Location"))
	case resp.Request.URL.Path == "/api/mods" && resp.StatusCode == http.StatusOK:
		return f.addToList(resp)
	}
	return nil
}

func (f *Fake) addToList(resp *http.Response) error {
	var doc map[string]json.RawMessage
	err := json.NewDecoder(resp.Body).Decode(&doc)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	var results []json.RawMessage
	if err := json.Unmarshal(doc["results"], &results); err != nil {
		return err
	}
	f.mu.Lock()
	for _, name := range f.added {
		results = append(results, listEntry(name, f.latest(name)))
	}
	f.mu.Unlock()
	doc["results"], _ = json.Marshal(results)
	body, _ := json.Marshal(doc)
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", fmt.Sprint(len(body)))
	return nil
}

// latest is the highest-sorting version added for a mod; the caller holds mu.
func (f *Fake) latest(name string) modportaltest.Served {
	var last modportaltest.Served
	for _, s := range f.served {
		if s.Name == name && (last.Name == "" || s.Version > last.Version) {
			last = s
		}
	}
	return last
}

func listEntry(name string, s modportaltest.Served) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"name": name, "title": name, "summary": "added by a test", "downloads_count": 1, "category": "content",
		"latest_release": map[string]any{
			"download_url": s.DownloadURL, "file_name": s.FileName, "version": s.Version, "sha1": s.SHA1,
			"released_at": "2026-01-01T00:00:00.000000Z",
			"info_json":   map[string]any{"factorio_version": s.FactorioVersion},
		},
	})
	return b
}

func makeCerts() (caPEM []byte, leaf tls.Certificate, err error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, leaf, err
	}
	now := time.Now()
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Foreman fake portal test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, leaf, err
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, leaf, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, leaf, err
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: portalHost},
		DNSNames:     []string{portalHost, "*.factorio.com"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, leaf, err
	}
	leaf = tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), leaf, nil
}
