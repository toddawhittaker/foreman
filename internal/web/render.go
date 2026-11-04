package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// Paths the templates link to or post to. The router serves these.
const (
	PathWorlds    = "/worlds"
	PathSignIn    = "/signin"
	PathSetup     = "/setup"
	PathSignOut   = "/signout"
	PathSignedOut = "/signed-out"
	PathStatic    = "/static/"
)

//go:embed templates/*.html
var templateFS embed.FS

// Everything the pages load ships inside the binary (SPEC 3.7).
//
//go:embed static
var staticFS embed.FS

// Page is the view model of one full page. Only the types in this file
// implement it, so a page can never be rendered with another page's data.
type Page interface {
	templateName() string
}

// Problem is one entry in a form's error summary (DESIGN 7.2): the id of
// the field at fault and the same words as that field's error.
type Problem struct {
	FieldID string
	Message string
}

// maxListRows is how many rows a saves or snapshots table shows before
// "Show older".
const maxListRows = 50

// WorldsPage is the world list (SPEC 5.4, DESIGN 8.2).
type WorldsPage struct {
	Worlds []world.Summary
	// Deleted names the world a delete just removed, for the message, and
	// DeletedKept says whether its snapshots remain under Deleted worlds.
	Deleted     string
	DeletedKept bool
}

// SignedOutPage confirms that sign-out ended the session (SPEC 12.8).
type SignedOutPage struct{}

// SignInPage is the local-account sign-in form (SPEC 12.3, 12.4).
type SignInPage struct {
	// Username refills the field after a failed attempt. The password is
	// never sent back.
	Username      string
	UsernameError string
	PasswordError string
	// RateLimited replaces the field errors with a message to wait
	// (SPEC 12.4). RetryAfter is how long; zero means unknown.
	RateLimited bool
	RetryAfter  time.Duration
	// Busy replaces the field errors with a message to try again shortly:
	// too many password checks were already running.
	Busy bool
}

// SetupPage creates the first admin account on first run (SPEC 12.3).
type SetupPage struct {
	Username      string
	UsernameError string
	PasswordError string
	ConfirmError  string
	// MinPasswordLength, when above zero, is shown as a hint on the
	// password field.
	MinPasswordLength int
}

// ErrorPage is a whole-page error for 403, 404, 500 and any other status.
// Detail, when set, replaces the default explanation for the status; it
// must never contain a secret or an internal error string.
type ErrorPage struct {
	Status int
	Detail string
}

func (WorldsPage) templateName() string    { return "worlds" }
func (SignedOutPage) templateName() string { return "signedout" }
func (SignInPage) templateName() string    { return "signin" }
func (SetupPage) templateName() string     { return "setup" }
func (ErrorPage) templateName() string     { return "error" }

func (p SignInPage) hasErrors() bool { return p.RateLimited || p.Busy || len(p.Problems()) > 0 }
func (p SetupPage) hasErrors() bool  { return len(p.Problems()) > 0 }

func (WorldsPage) navItem() string { return PathWorlds }

// Problems lists the field errors in the order the fields appear.
func (p SignInPage) Problems() []Problem {
	if p.RateLimited || p.Busy {
		return nil
	}
	return problems(
		Problem{"username", p.UsernameError},
		Problem{"password", p.PasswordError},
	)
}

// Wait says how long to wait before trying to sign in again.
func (p SignInPage) Wait() string {
	if p.RetryAfter <= 0 {
		return "a few minutes"
	}
	m := int((p.RetryAfter + time.Minute - 1) / time.Minute)
	if m == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", m)
}

// Problems lists the field errors in the order the fields appear.
func (p SetupPage) Problems() []Problem {
	return problems(
		Problem{"username", p.UsernameError},
		Problem{"password", p.PasswordError},
		Problem{"confirm", p.ConfirmError},
	)
}

// PasswordDescribedBy lists the ids of the password field's hint and
// error, whichever are shown.
func (p SetupPage) PasswordDescribedBy() string {
	var ids []string
	if p.MinPasswordLength > 0 {
		ids = append(ids, "password-hint")
	}
	if p.PasswordError != "" {
		ids = append(ids, "password-error")
	}
	return strings.Join(ids, " ")
}

func problems(all ...Problem) []Problem {
	var out []Problem
	for _, pr := range all {
		if pr.Message != "" {
			out = append(out, pr)
		}
	}
	return out
}

// Heading is the page's h1 and title for its status.
func (p ErrorPage) Heading() string {
	switch p.Status {
	case http.StatusForbidden:
		return "Access refused"
	case http.StatusNotFound:
		return "Page not found"
	case http.StatusInternalServerError:
		return "Something went wrong"
	default:
		return "Request failed"
	}
}

// Explanation says what happened and what to do next (DESIGN 9).
func (p ErrorPage) Explanation() string {
	if p.Detail != "" {
		return p.Detail
	}
	switch p.Status {
	case http.StatusForbidden:
		return "Foreman refused this request. If you followed a link from another site, open Foreman directly and try again. If it keeps happening, ask the person who runs Foreman to check your access."
	case http.StatusNotFound:
		return "There is no page at this address. Check the address, or go to the home page."
	case http.StatusInternalServerError:
		return "Foreman could not finish this request. The details are in Foreman's log. Try again, and if it fails again, check the log."
	default:
		return "Foreman could not finish this request. Go to the home page and try again."
	}
}

// view is what every template receives: the signed-in user for the layout
// and the page's own view model.
type view struct {
	User string
	Page Page
}

// Current is true when the nav link to path names this page, so it gets
// aria-current (DESIGN 6).
func (v view) Current(path string) bool {
	n, ok := v.Page.(interface{ navItem() string })
	return ok && n.navItem() == path
}

// LiveURL is where the page's live updates stream from, or "" for a page
// that has none (SPEC 16.3).
func (v view) LiveURL() string {
	l, ok := v.Page.(interface{ liveURL() string })
	if !ok {
		return ""
	}
	return l.liveURL()
}

// Failed is true when a form page is showing errors, so the title can say
// so first (WCAG 3.3.1 and 2.4.2).
func (v view) Failed() bool {
	f, ok := v.Page.(interface{ hasErrors() bool })
	return ok && f.hasErrors()
}

type staticFile struct {
	body        []byte
	contentType string
	hash        string
}

// Only these types are served, so SHA256SUMS and anything else stays private.
var staticTypes = map[string]string{
	".css": "text/css; charset=utf-8",
	".js":  "text/javascript; charset=utf-8",
}

// Renderer holds the parsed templates and the embedded static files.
type Renderer struct {
	pages  map[string]*template.Template
	static map[string]staticFile
}

// NewRenderer parses every template and hashes every static file once.
func NewRenderer() (*Renderer, error) {
	r := &Renderer{pages: map[string]*template.Template{}, static: map[string]staticFile{}}
	if err := r.loadStatic(); err != nil {
		return nil, err
	}
	funcs := template.FuncMap{
		"static":      r.staticURL,
		"serverPath":  func() string { return PathServer },
		"consolePath": func() string { return PathConsole },
		"worldsPath":  func() string { return PathWorlds },
		"bytes":       world.FormatBytes,
		"utc":         func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") },
		"isoTime":     func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		// day and clock are a table's timestamp in two parts that each
		// never wrap (DESIGN 8.3); utcSeconds tells apart two snapshots
		// taken in the same minute.
		"day":           func(t time.Time) string { return t.UTC().Format("2006-01-02") },
		"clock":         func(t time.Time) string { return t.UTC().Format("15:04 UTC") },
		"utcSeconds":    func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") },
		"deletedPath":   func() string { return PathDeleted },
		"signInPath":    func() string { return PathSignIn },
		"setupPath":     func() string { return PathSetup },
		"signOutPath":   func() string { return PathSignOut },
		"signedOutPath": func() string { return PathSignedOut },
	}
	files, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".html")
		if name == "layout" {
			continue
		}
		t, err := template.New(name).Funcs(funcs).ParseFS(templateFS,
			"templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		r.pages[name] = t
	}
	return r, nil
}

func (r *Renderer) loadStatic() error {
	return fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ct, ok := staticTypes[path.Ext(p)]
		if !ok {
			return nil
		}
		body, err := staticFS.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		r.static[strings.TrimPrefix(p, "static/")] = staticFile{
			body:        body,
			contentType: ct,
			hash:        hex.EncodeToString(sum[:])[:16],
		}
		return nil
	})
}

// staticURL adds the file's content hash, so a new binary with a changed
// file gets a new URL while unchanged files stay cached.
func (r *Renderer) staticURL(name string) (string, error) {
	f, ok := r.static[name]
	if !ok {
		return "", fmt.Errorf("no static file %q", name)
	}
	return PathStatic + name + "?v=" + f.hash, nil
}

// Render writes page p as a complete HTML document with the given status.
// user is the signed-in username, or empty to hide the sign-out form. The
// page is rendered in full before anything is written, so on error the
// response is untouched and the caller can still send an ErrorPage.
func (r *Renderer) Render(w http.ResponseWriter, status int, user string, p Page) error {
	t, ok := r.pages[p.templateName()]
	if !ok {
		return fmt.Errorf("no template for %T", p)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", view{User: user, Page: p}); err != nil {
		return fmt.Errorf("render %s: %w", p.templateName(), err)
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// Pages can show who is signed in; the back button after sign-out must not.
	h.Set("Cache-Control", "no-store")
	// Pages carry sign-in forms, so no other site may frame them.
	h.Set("Content-Security-Policy", "frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// Fragment writes one named template of page p on its own, for a live
// update of part of the page.
func (r *Renderer) Fragment(w io.Writer, p Page, name string) error {
	t, ok := r.pages[p.templateName()]
	if !ok {
		return fmt.Errorf("no template for %T", p)
	}
	return t.ExecuteTemplate(w, name, p)
}

// Static serves the embedded CSS and JavaScript under PathStatic. A request
// carrying the current content hash is cached for a year; any other is
// revalidated.
func (r *Renderer) Static() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, PathStatic)
		f, ok := r.static[name]
		if !ok {
			http.NotFound(w, req)
			return
		}
		h := w.Header()
		h.Set("Content-Type", f.contentType)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("ETag", `"`+f.hash+`"`)
		if req.URL.Query().Get("v") == f.hash {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		http.ServeContent(w, req, name, time.Time{}, bytes.NewReader(f.body))
	})
}
