package modportal

import (
	"context"
	"crypto/sha1" //nolint:gosec // the portal publishes SHA-1 sums (SPEC 9.12)
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Download writes one release's zip to dst and checks it against the SHA-1
// the portal published (SPEC 9.12). On any failure dst is truncated; the
// caller removes the file. The username and token go in the query string, as
// the portal asks, so no error returned from here carries a URL.
func (c *Client) Download(ctx context.Context, username, token string, r Release, dst *os.File) error {
	secrets := []string{token, username}
	if err := checkRelease(r); err != nil {
		return err
	}
	dlPath, err := downloadPath(r.DownloadURL)
	if err != nil {
		return err
	}
	q := url.Values{"username": {username}, "token": {token}}

	ctx, cancel := context.WithTimeout(ctx, c.downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+dlPath+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("%w: bad address", ErrUnreachable)
	}
	resp, err := c.dl.Do(req)
	if err != nil {
		return requestError(err, secrets...)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case http.StatusNotFound:
		return fmt.Errorf("%w: the file is gone", ErrNotOnPortal)
	default:
		return &statusError{resp.StatusCode}
	}
	if resp.ContentLength > c.maxDownload {
		return fmt.Errorf("%w: the file is too large", ErrUnreachable)
	}

	h := sha1.New() //nolint:gosec // see import
	out := &recordingWriter{w: io.MultiWriter(dst, h)}
	n, err := io.Copy(out, io.LimitReader(resp.Body, c.maxDownload+1))
	switch {
	case out.err != nil:
		_ = dst.Truncate(0)
		return fmt.Errorf("modportal: writing the file: %w", out.err)
	case err != nil:
		_ = dst.Truncate(0)
		return requestError(err, secrets...)
	case n > c.maxDownload:
		_ = dst.Truncate(0)
		return fmt.Errorf("%w: the file is too large", ErrUnreachable)
	case r.SHA1 == "" || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), r.SHA1):
		_ = dst.Truncate(0)
		return ErrChecksum
	}
	return nil
}

// checkRelease refuses a file name that is not <name>_<version>.zip, or that
// could reach outside the mods folder once the caller joins it to a path.
func checkRelease(r Release) error {
	suffix := "_" + r.Version + ".zip"
	if !safeName(r.Version) || !safeName(r.FileName) ||
		!strings.HasSuffix(r.FileName, suffix) || len(r.FileName) == len(suffix) {
		return fmt.Errorf("modportal: refusing release file name %q", r.FileName)
	}
	return nil
}

// downloadPath accepts only a plain path under /download/, so the request
// stays on the fixed base.
func downloadPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.RawQuery != "" || u.Fragment != "" ||
		!strings.HasPrefix(u.Path, "/download/") || strings.Contains(u.Path, "..") ||
		strings.ContainsAny(raw, `\`) {
		return "", fmt.Errorf("modportal: refusing download address %q", raw)
	}
	return u.EscapedPath(), nil
}

// recordingWriter remembers a write failure, so it is not mistaken for a
// network failure.
type recordingWriter struct {
	w   io.Writer
	err error
}

func (rw *recordingWriter) Write(p []byte) (int, error) {
	n, err := rw.w.Write(p)
	if err != nil {
		rw.err = err
	}
	return n, err
}
