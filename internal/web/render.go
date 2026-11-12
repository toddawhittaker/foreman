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
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Paths the templates link to or post to. The router serves these.
const (
	PathWorlds    = "/worlds"
	PathSignIn    = "/signin"
	PathSetup     = "/setup"
	PathSignOut   = "/signout"
	PathSignedOut = "/signed-out"
	PathLanguage  = "/language"
	PathStatic    = "/static/"
	PathFavicon   = "/favicon.ico"
)

// faviconFile is the static file answered at PathFavicon.
const faviconFile = "icons/favicon.ico"

//go:embed templates/*.html
var templateFS embed.FS

// Everything the pages load ships inside the binary (SPEC 3.7).
//
//go:embed static
var staticFS embed.FS

// jsKeys lists the catalogue keys the page scripts use; layout.html puts
// their text in the #i18n JSON block. jsstrings.go fills it in an init
// function, so the scripts' keys live in one file of their own.
var jsKeys []string

// Page is the view model of one full page. Only the types in this package
// implement it, so a page can never be rendered with another page's data.
type Page interface {
	templateName() string
}

// Problem is one entry in a form's error summary (DESIGN 7.2): the id of
// the field at fault and the same words as that field's error.
type Problem struct {
	FieldID string
	Message i18n.Msg
}

// maxListRows is how many rows a saves or snapshots table shows before
// "Show older".
const maxListRows = 50

// SignedOutPage confirms that sign-out ended the session (SPEC 12.8).
type SignedOutPage struct{}

// SignInPage is the local-account sign-in form (SPEC 12.3, 12.4).
type SignInPage struct {
	// Username refills the field after a failed attempt. The password is
	// never sent back.
	Username      string
	UsernameError i18n.Msg
	PasswordError i18n.Msg
	// Next is the page to return to after signing in; empty means home
	// (SPEC 14.17). It has already passed returnPath.
	Next string
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
	UsernameError i18n.Msg
	PasswordError i18n.Msg
	ConfirmError  i18n.Msg
	// MinPasswordLength, when above zero, is shown as a hint on the
	// password field.
	MinPasswordLength int
}

// ErrorPage is a whole-page error for 403, 404, 500 and any other status.
// Detail, when set, replaces the default explanation for the status; it
// must never contain a secret or an internal error string.
type ErrorPage struct {
	Status int
	Detail i18n.Msg
}

func (SignedOutPage) templateName() string { return "signedout" }
func (SignInPage) templateName() string    { return "signin" }
func (SetupPage) templateName() string     { return "setup" }
func (ErrorPage) templateName() string     { return "error" }

func (p SignInPage) hasErrors() bool { return p.RateLimited || p.Busy || len(p.Problems()) > 0 }
func (p SetupPage) hasErrors() bool  { return len(p.Problems()) > 0 }

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

// RateLimitMessage says how long to wait before trying to sign in again.
func (p SignInPage) RateLimitMessage() i18n.Msg {
	if p.RetryAfter <= 0 {
		return i18n.M("accounts.signin.ratelimited.unknown")
	}
	m := int((p.RetryAfter + time.Minute - 1) / time.Minute)
	return i18n.M("accounts.signin.ratelimited.minutes", "count", m)
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
	if !p.PasswordError.IsZero() {
		ids = append(ids, "password-error")
	}
	return strings.Join(ids, " ")
}

func problems(all ...Problem) []Problem {
	var out []Problem
	for _, pr := range all {
		if !pr.Message.IsZero() {
			out = append(out, pr)
		}
	}
	return out
}

// Heading is the page's h1 and title for its status.
func (p ErrorPage) Heading() i18n.Msg {
	switch p.Status {
	case http.StatusForbidden:
		return i18n.M("common.error.forbidden.heading")
	case http.StatusNotFound:
		return i18n.M("common.error.notfound.heading")
	case http.StatusInternalServerError:
		return i18n.M("common.error.internal.heading")
	default:
		return i18n.M("common.error.other.heading")
	}
}

// Explanation says what happened and what to do next (DESIGN 9).
func (p ErrorPage) Explanation() i18n.Msg {
	if !p.Detail.IsZero() {
		return p.Detail
	}
	switch p.Status {
	case http.StatusForbidden:
		return i18n.M("common.error.forbidden.text")
	case http.StatusNotFound:
		return i18n.M("common.error.notfound.text")
	case http.StatusInternalServerError:
		return i18n.M("common.error.internal.text")
	default:
		return i18n.M("common.error.other.text")
	}
}

// view is what every template receives: the signed-in user for the layout
// and the page's own view model.
type view struct {
	User string
	Page Page
	// Return is the page the language menu comes back to, else empty, which
	// means home (see menuReturn).
	Return string
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
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".png":   "image/png",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
}

// Renderer holds the parsed templates, once per language, and the
// embedded static files.
type Renderer struct {
	pages  map[i18n.Locale]map[string]*template.Template
	static map[string]staticFile
}

// sharedFuncs are the template functions that do not depend on the language.
func (r *Renderer) sharedFuncs() template.FuncMap {
	return template.FuncMap{
		"static":           r.staticURL,
		"serverPath":       func() string { return PathServer },
		"consolePath":      func() string { return PathConsole },
		"worldsPath":       func() string { return PathWorlds },
		"snapshotsTabPath": func() string { return PathSnapshots },
		"accountPath":      func() string { return PathAccount },
		"utc":              func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") },
		"isoTime":          func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		// day and clock are a table's timestamp in two parts that each
		// never wrap (DESIGN 8.3).
		"day":   func(t time.Time) string { return t.UTC().Format("2006-01-02") },
		"clock": func(t time.Time) string { return t.UTC().Format("15:04 UTC") },
		// momentUTC and momentUTCSeconds are a <time> element for a sentence
		// that holds a time, for tHTML.
		"momentUTC": momentUTC,
		"momentUTCSeconds": func(t time.Time) i18n.Fragment {
			return i18n.Moment(t.UTC().Format(time.RFC3339), t.UTC().Format("2006-01-02 15:04:05 UTC"))
		},
		"deletedPath":   func() string { return PathDeleted },
		"signInPath":    func() string { return PathSignIn },
		"setupPath":     func() string { return PathSetup },
		"signOutPath":   func() string { return PathSignOut },
		"signedOutPath": func() string { return PathSignedOut },
		"languagePath":  func() string { return PathLanguage },
		"languages":     func() []i18n.Language { return i18n.Languages },
		// english is a key's English, for a word shown in English beside
		// the page's language, as the language menu's label is.
		"english": func(key string) string { return i18n.English.T(key) },
	}
}

// localeFuncs are the template functions that speak loc's language
// (SPEC 22.3, 22.4).
func localeFuncs(loc i18n.Locale) template.FuncMap {
	return template.FuncMap{
		// t translates a key: {{t "common.signout"}}, or with arguments
		// {{t "common.signedin" "user" .User}}.
		"t": loc.T,
		// tHTML is one sentence with markup at its placeholders, from link
		// and code: {{tHTML "key" "link" (link "/href" "linkKey") "file" (code .Path)}}.
		"tHTML":  loc.HTML,
		"link":   loc.Link,
		"code":   i18n.Code,
		"codes":  loc.Codes,
		"nowrap": i18n.NoWrap,
		"strong": i18n.Strong,
		// ident wraps a name a program reads, such as a mod's, so it never
		// hyphenates; idents is a list of them.
		"ident":  i18n.Ident,
		"idents": loc.Idents,
		// extLink links to an https address off this site, in a new tab.
		"extLink": loc.ExternalLink,
		// portalPath is a mod's page on the mod portal.
		"portalPath": portalPath,
		// game marks Factorio's own words, for tHTML.
		"game": i18n.Game,
		// m makes a Msg to fill a placeholder, such as a snapshot label:
		// {{t "key" "label" (m "worlds.snapshot.label.before_restore")}}.
		"m": i18n.M,
		// msg renders an i18n.Msg from a view model; msgHTML one whose
		// arguments hold fragments.
		"msg":      loc.Format,
		"msgHTML":  loc.FormatHTML,
		"bytes":    loc.Bytes,
		"num":      loc.Num,
		"decimal1": loc.OneDecimal,
		"duration": loc.Duration,
		// snapLabel and snapReason show a snapshot's label and why it was
		// taken (see snapshotlabel.go).
		"snapLabel":  func(s world.Snapshot) string { return snapshotLabel(loc, s) },
		"snapReason": func(s world.Snapshot) string { return snapshotReason(loc, s) },
		"lang":       loc.String,
		"jsStrings": func() map[string]any {
			out := make(map[string]any, len(jsKeys))
			for _, k := range jsKeys {
				out[k] = loc.Entry(k)
			}
			return out
		},
	}
}

// partials are template files that no route renders on their own: every page
// loads them, and each defines templates the pages include.
var partials = []string{"mapgenform", "generatepreview", "failure", "modportal"}

var partialFiles = func() []string {
	out := make([]string, len(partials))
	for i, p := range partials {
		out[i] = "templates/" + p + ".html"
	}
	return out
}()

func isPartial(name string) bool { return slices.Contains(partials, name) }

// NewRenderer parses every template once, makes a copy for each language
// with that language's functions, and hashes every static file once.
func NewRenderer() (*Renderer, error) {
	r := &Renderer{pages: map[i18n.Locale]map[string]*template.Template{}, static: map[string]staticFile{}}
	if err := r.loadStatic(); err != nil {
		return nil, err
	}
	funcs := r.sharedFuncs()
	for k, f := range localeFuncs(i18n.English) {
		funcs[k] = f
	}
	files, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, l := range i18n.Languages {
		r.pages[l.Tag] = map[string]*template.Template{}
	}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".html")
		if name == "layout" || isPartial(name) {
			continue
		}
		base, err := template.New(name).Funcs(funcs).ParseFS(templateFS,
			append([]string{"templates/layout.html", "templates/" + name + ".html"}, partialFiles...)...)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		for _, l := range i18n.Languages {
			t, err := base.Clone()
			if err != nil {
				return nil, fmt.Errorf("copy template %s: %w", name, err)
			}
			r.pages[l.Tag][name] = t.Funcs(localeFuncs(l.Tag))
		}
	}
	return r, nil
}

func (r *Renderer) template(loc i18n.Locale, p Page) (*template.Template, error) {
	pages, ok := r.pages[loc]
	if !ok {
		pages = r.pages[i18n.English]
	}
	t, ok := pages[p.templateName()]
	if !ok {
		return nil, fmt.Errorf("no template for %T", p)
	}
	return t, nil
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

// menuReturn is the address the language menu goes back to. A GET page is
// its own address. Any other request came from a form, whose address answers
// only POST, so it is the page the form was on, from a same-site Referer.
// Without a usable one it is empty, which means home (SPEC 22.2, 14.17).
func menuReturn(req *http.Request) string {
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		return languageReturn(req.URL.RequestURI())
	}
	ref, err := url.Parse(req.Referer())
	if err != nil || ref.Host == "" || ref.Host != req.Host {
		return ""
	}
	return languageReturn(ref.RequestURI())
}

// Render writes page p as a complete HTML document with the given status,
// in the language of req (localeOf). user is the signed-in username, or
// empty to hide the sign-out form. The page is rendered in full before
// anything is written, so on error the response is untouched and the
// caller can still send an ErrorPage.
func (r *Renderer) Render(w http.ResponseWriter, req *http.Request, status int, user string, p Page) error {
	t, err := r.template(localeOf(req), p)
	if err != nil {
		return err
	}
	v := view{User: user, Page: p, Return: menuReturn(req)}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		return fmt.Errorf("render %s: %w", p.templateName(), err)
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// Pages can show who is signed in; the back button after sign-out must not.
	h.Set("Cache-Control", "no-store")
	// Pages carry sign-in forms, so no other site may frame them.
	h.Set("Content-Security-Policy", "frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	// The language follows a cookie and the request's Accept-Language.
	h.Set("Vary", "Cookie, Accept-Language")
	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	return err
}

// Fragment writes one named template of page p on its own, in loc's
// language, for a live update of part of the page.
func (r *Renderer) Fragment(w io.Writer, loc i18n.Locale, p Page, name string) error {
	t, err := r.template(loc, p)
	if err != nil {
		return err
	}
	return t.ExecuteTemplate(w, name, p)
}

// Static serves the embedded CSS, JavaScript and icons under PathStatic. A
// request carrying the current content hash is cached for a year; any other
// is revalidated.
func (r *Renderer) Static() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.serveStatic(w, req, strings.TrimPrefix(req.URL.Path, PathStatic))
	})
}

// Favicon answers the request browsers make at the site root, whatever the
// page's own icon links say.
func (r *Renderer) Favicon() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.serveStatic(w, req, faviconFile)
	})
}

func (r *Renderer) serveStatic(w http.ResponseWriter, req *http.Request, name string) {
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
}

// momentUTC is a <time> element for t in UTC, for a sentence that holds a
// time (SPEC 17.11).
func momentUTC(t time.Time) i18n.Fragment {
	return i18n.Moment(t.UTC().Format(time.RFC3339), t.UTC().Format("2006-01-02 15:04 UTC"))
}
