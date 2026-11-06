// Package i18n holds Foreman's translated text and formats it for one
// language (SPEC 22).
//
// # Catalogues
//
// Text lives in locales/<lang>/<area>.json, one file per area (common,
// accounts, server, worlds, js) in each of the eight languages. A file is
// a flat JSON object from a stable dotted key to a value. Keys start with
// their area ("accounts.signin.title"), and a key lives in one area file
// only. A value is either a string or, when it counts something, an
// object from CLDR plural category ("one", "few", "many", "other", ...)
// to a string, chosen by the "count" argument. Each language has exactly
// the categories its CLDR rules use for whole numbers. Values hold named
// placeholders such as {world} and never HTML.
//
// Adding a string: add the key to en and to the seven other languages in
// the same area file, then run make i18n-hash, which records in
// translated/<area>.json which English each translation was made from. When the
// English of a key changes later, the check fails until every language is
// updated and make i18n-hash is run again.
//
// # Using it
//
// Go code that builds text for a page returns a Msg, made with M, never a
// string: i18n.M("accounts.setup.password.short", "count", 12). The key
// must be a string literal, so the checks can see it. An argument that is
// itself a Msg is translated when it is filled in. Numbers are formatted
// for the language, and a Size is a number of bytes in the language's units.
//
// Templates (internal/web) call {{t "key" "name" value}}, {{msg .Field}}
// for a Msg from the view model, and for a sentence holding markup
// {{tHTML "key" "link" (link "/href" "linkKey") "file" (code .Path)}}:
// link, code, codes, strong, nowrap and momentUTC (a time element) make
// trusted fragments, and tHTML escapes the sentence and plain arguments and
// puts each fragment at its placeholder. A field error is
// shown with {{if not .Err.IsZero}}...{{msg .Err}}{{end}}, as a Msg is
// never empty to {{with}}. bytes, num and duration format a size, a number
// and a time span; lang is the page's tag.
//
// Scripts read their text from the #i18n JSON block of each page: a map
// from key to text, or to plural forms by category, placeholders unfilled.
// A key appears there once it is listed in internal/web's jsKeys.
//
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

//go:embed locales
var localeFS embed.FS

// Locale is one of the supported language tags, such as "de" or
// "zh-Hans". Any other value, including "", behaves as English.
type Locale string

// English is the fallback language and the one the catalogue is written in.
const English Locale = "en"

// Language is a supported language and its name in that language, as the
// language menu shows it.
type Language struct {
	Tag  Locale
	Name string
}

// Languages lists the supported languages in menu order (SPEC 22.1).
var Languages = []Language{
	{"en", "English"},
	{"de", "Deutsch"},
	{"ru", "Русский"},
	{"zh-Hans", "简体中文"},
	{"fr", "Français"},
	{"es", "Español"},
	{"pl", "Polski"},
	{"cs", "Čeština"},
}

// Msg is a piece of text for the user: a catalogue key and its arguments.
type Msg struct {
	Key  string
	Args map[string]any
}

// M builds a Msg from a key and pairs of argument name and value.
func M(key string, kv ...any) Msg {
	m := Msg{Key: key}
	if len(kv) > 0 {
		m.Args = make(map[string]any, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			name, _ := kv[i].(string)
			m.Args[name] = kv[i+1]
		}
	}
	return m
}

// IsZero reports whether m holds no text, as an unset error field does.
func (m Msg) IsZero() bool { return m.Key == "" }

// entry is one catalogue value: text, or plural forms by category.
type entry struct {
	text   string
	plural map[string]string
}

func (e *entry) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &e.text); err == nil {
		return nil
	}
	return json.Unmarshal(b, &e.plural)
}

// catalogue holds every language's entries by key, loaded once.
var catalogue = mustLoad()

func mustLoad() map[Locale]map[string]entry {
	c, err := load()
	if err != nil {
		panic(err)
	}
	return c
}

// load reads every catalogue file. A key in two files of one language is
// an error.
func load() (map[Locale]map[string]entry, error) {
	files, err := fs.Glob(localeFS, "locales/*/*.json")
	if err != nil {
		return nil, err
	}
	out := map[Locale]map[string]entry{}
	for _, f := range files {
		lang := Locale(path.Base(path.Dir(f)))
		b, err := localeFS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var entries map[string]entry
		if err := json.Unmarshal(b, &entries); err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		if out[lang] == nil {
			out[lang] = map[string]entry{}
		}
		for k, e := range entries {
			if _, dup := out[lang][k]; dup {
				return nil, fmt.Errorf("%s: key %q is in another file too", f, k)
			}
			out[lang][k] = e
		}
	}
	return out, nil
}

// EnglishKeys lists every key in the English catalogue, sorted.
func EnglishKeys() []string {
	keys := make([]string, 0, len(catalogue[English]))
	for k := range catalogue[English] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var supported = func() []language.Tag {
	tags := make([]language.Tag, len(Languages))
	for i, l := range Languages {
		tags[i] = language.MustParse(string(l.Tag))
	}
	return tags
}()

var matcher = language.NewMatcher(supported)

// Parse returns the Locale for tag when it is exactly one of the supported
// tags.
func Parse(tag string) (Locale, bool) {
	for _, l := range Languages {
		if string(l.Tag) == tag {
			return l.Tag, true
		}
	}
	return English, false
}

// Match picks the language for a request (SPEC 22.2): the cookie's tag
// when it is a supported one, else the best match for the Accept-Language
// header, else English.
func Match(cookie, acceptLanguage string) Locale {
	if l, ok := Parse(cookie); ok {
		return l
	}
	wanted := parseAcceptLanguage(acceptLanguage)
	if len(wanted) == 0 {
		return English
	}
	_, i, conf := matcher.Match(wanted...)
	if conf == language.No {
		return English
	}
	return Languages[i].Tag
}

// maxAcceptEntries caps how many comma-separated entries of an
// Accept-Language header are read, so a hostile header costs little.
const maxAcceptEntries = 32

// parseAcceptLanguage returns the tags of header in order of preference.
// Each entry is parsed alone, so an unknown or malformed one is skipped
// instead of discarding the whole header.
func parseAcceptLanguage(header string) []language.Tag {
	type weighted struct {
		tag language.Tag
		q   float32
	}
	// The extra element holds the unread rest of the header.
	entries := strings.SplitN(header, ",", maxAcceptEntries+1)
	if len(entries) > maxAcceptEntries {
		entries = entries[:maxAcceptEntries]
	}
	var found []weighted
	for _, entry := range entries {
		tags, qs, err := language.ParseAcceptLanguage(entry)
		if err != nil {
			continue
		}
		for i := range tags {
			found = append(found, weighted{tags[i], qs[i]})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].q > found[j].q })
	tags := make([]language.Tag, len(found))
	for i, f := range found {
		tags[i] = f.tag
	}
	return tags
}

func (l Locale) known() Locale {
	if _, ok := catalogue[l]; ok && l != "" {
		return l
	}
	return English
}

func (l Locale) tag() language.Tag { return language.MustParse(string(l.known())) }

// String is the BCP 47 tag, for the page's lang attribute.
func (l Locale) String() string { return string(l.known()) }

// lookup finds key in l, falling back to English and logging the gap. A
// key missing from English too is returned as the key itself.
func (l Locale) lookup(key string) (entry, Locale) {
	l = l.known()
	if e, ok := catalogue[l][key]; ok {
		return e, l
	}
	slog.Error("i18n: key missing from catalogue", "key", key, "lang", string(l))
	if e, ok := catalogue[English][key]; ok {
		return e, English
	}
	return entry{text: key}, English
}

// T translates key with pairs of argument name and value.
func (l Locale) T(key string, kv ...any) string { return l.Format(M(key, kv...)) }

// Format translates m.
func (l Locale) Format(m Msg) string {
	if m.Key == "" {
		return ""
	}
	e, from := l.lookup(m.Key)
	return l.fill(from.pattern(e, m), m.Args)
}

// pattern picks the text of e for m, choosing the plural form by "count".
func (l Locale) pattern(e entry, m Msg) string {
	if e.plural == nil {
		return e.text
	}
	n, ok := toInt(m.Args["count"])
	if !ok {
		slog.Error("i18n: count must be an int", "key", m.Key)
	}
	if s, ok := e.plural[Category(l, n)]; ok {
		return s
	}
	return e.plural["other"]
}

// Category is the CLDR cardinal plural category of the whole number n in l.
func Category(l Locale, n int) string {
	if n < 0 {
		n = -n
	}
	switch plural.Cardinal.MatchPlural(l.tag(), n, 0, 0, 0, 0) {
	case plural.Zero:
		return "zero"
	case plural.One:
		return "one"
	case plural.Two:
		return "two"
	case plural.Few:
		return "few"
	case plural.Many:
		return "many"
	default:
		return "other"
	}
}

// eachPart walks pattern p: text gets each run of literal text, and arg
// each {name}, with the placeholder as written for when it has no value.
func eachPart(p string, text func(string), arg func(name, written string)) {
	for {
		open := strings.IndexByte(p, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(p[open:], '}')
		if end < 0 {
			break
		}
		text(p[:open])
		arg(p[open+1:open+end], p[open:open+end+1])
		p = p[open+end+1:]
	}
	text(p)
}

// fill replaces each {name} in p with its argument, formatted for l.
func (l Locale) fill(p string, args map[string]any) string {
	var b strings.Builder
	eachPart(p, func(s string) { b.WriteString(s) }, func(name, written string) {
		if v, ok := args[name]; ok {
			b.WriteString(l.arg(v))
		} else {
			slog.Error("i18n: no value for placeholder", "placeholder", name)
			b.WriteString(written)
		}
	})
	return b.String()
}

func (l Locale) arg(v any) string {
	switch v := v.(type) {
	case Msg:
		return l.Format(v)
	case Fragment:
		return v.text
	case string:
		return v
	case Size:
		return l.Bytes(uint64(v))
	case int, int64, int32, uint, uint64, uint32:
		return l.Num(v)
	default:
		return fmt.Sprint(v)
	}
}

func toInt(v any) (int, bool) {
	switch v := v.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case int32:
		return int(v), true
	}
	return 0, false
}

// Num formats a number with l's digit grouping and decimal mark.
func (l Locale) Num(n any) string {
	return message.NewPrinter(l.tag()).Sprint(number.Decimal(n))
}

// OneDecimal formats v with exactly one fraction digit, such as a
// percentage: "12.0" in English, "12,0" in German.
func (l Locale) OneDecimal(v float64) string {
	return message.NewPrinter(l.tag()).Sprint(number.Decimal(v,
		number.MinFractionDigits(1), number.MaxFractionDigits(1)))
}

// Bytes formats a size in decimal units with one fraction digit, as
// world.FormatBytes does in English: "512 B", "1.5 MB".
func (l Locale) Bytes(n uint64) string {
	const unit = 1000
	if n < unit {
		return l.T("common.bytes.b", "n", l.Num(n))
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	// Round as English always has, then only change the decimal mark.
	v, _ := strconv.ParseFloat(strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64), 64)
	s := message.NewPrinter(l.tag()).Sprint(number.Decimal(v,
		number.MinFractionDigits(1), number.MaxFractionDigits(1), number.NoSeparator()))
	switch exp {
	case 0:
		return l.T("common.bytes.kb", "n", s)
	case 1:
		return l.T("common.bytes.mb", "n", s)
	case 2:
		return l.T("common.bytes.gb", "n", s)
	case 3:
		return l.T("common.bytes.tb", "n", s)
	case 4:
		return l.T("common.bytes.pb", "n", s)
	default:
		return l.T("common.bytes.eb", "n", s)
	}
}

// Duration says d in its largest whole unit: seconds under a minute,
// minutes under an hour, else hours.
func (l Locale) Duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return l.T("common.duration.seconds", "count", int(d/time.Second))
	case d < time.Hour:
		return l.T("common.duration.minutes", "count", int(d/time.Minute))
	default:
		return l.T("common.duration.hours", "count", int(d/time.Hour))
	}
}

// Fragment is a piece of trusted markup for a placeholder of HTML. Only
// the helpers in this package make one (Link, Code, Codes, Game, Moment,
// NoWrap and Strong), and each escapes what it wraps.
type Fragment struct{ html, text string }

// Link is a link to href, which must be one of Foreman's own paths, whose
// text is linkKey's translation. Any other href is refused: the text is
// shown without a link, and under test it panics, so no page ships one.
func (l Locale) Link(href, linkKey string) Fragment {
	esc := template.HTMLEscapeString
	text := l.T(linkKey)
	if !ownPath(href) {
		if testing.Testing() {
			panic("i18n: Link to a path that is not Foreman's own: " + href)
		}
		slog.Error("i18n: link refused, not one of Foreman's own paths", "key", linkKey)
		return Fragment{esc(text), text}
	}
	return Fragment{`<a href="` + esc(href) + `">` + esc(text) + `</a>`, text}
}

// ownPath reports whether href is a path on this site: one slash, then
// not a second slash or a backslash, which browsers read as another host.
func ownPath(href string) bool {
	return strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") && !strings.HasPrefix(href, "/\\")
}

// Code wraps s, such as a file name, in a code element.
func Code(s string) Fragment {
	return Fragment{"<code>" + template.HTMLEscapeString(s) + "</code>", s}
}

// Game marks s as Factorio's own words, which stay English (SPEC 22.3).
func Game(s string) Fragment {
	return Fragment{`<span lang="en">` + template.HTMLEscapeString(s) + `</span>`, s}
}

// HTML translates key as T does, for a sentence that holds markup. The
// sentence and every plain argument are escaped; each Fragment argument
// goes in as it is, at its placeholder, so the translator decides where
// the markup goes in the sentence.
func (l Locale) HTML(key string, kv ...any) template.HTML { return l.FormatHTML(M(key, kv...)) }

// FormatHTML is HTML for a Msg, so a view model can carry a sentence with
// fragments. A Msg argument is filled in the same way, markup and all.
func (l Locale) FormatHTML(m Msg) template.HTML {
	if m.Key == "" {
		return ""
	}
	e, from := l.lookup(m.Key)
	esc := template.HTMLEscapeString
	var b strings.Builder
	eachPart(from.pattern(e, m), func(s string) { b.WriteString(esc(s)) }, func(name, written string) {
		v, ok := m.Args[name]
		if !ok {
			slog.Error("i18n: no value for placeholder", "placeholder", name)
			b.WriteString(esc(written))
			return
		}
		switch v := v.(type) {
		case Fragment:
			b.WriteString(v.html)
		case Msg:
			b.WriteString(string(l.FormatHTML(v)))
		default:
			b.WriteString(esc(l.arg(v)))
		}
	})
	return template.HTML(b.String()) //nolint:gosec // text and plain arguments are escaped; fragments escape what they wrap
}

// Entry is key's value for a script: a string, or plural forms by
// category, with the placeholders left unfilled.
func (l Locale) Entry(key string) any {
	e, _ := l.lookup(key)
	if e.plural != nil {
		return e.plural
	}
	return e.text
}
