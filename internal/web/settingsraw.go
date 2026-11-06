package web

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// maxLineEcho is how much of a faulty line an error quotes.
const maxLineEcho = 120

// SettingsRawPage is the JSON editor for server-settings.json (SPEC 11.2,
// DESIGN 8.10).
type SettingsRawPage struct {
	WorldID, WorldName string
	// ReadOnly says why nothing can be saved; zero when the world is open to
	// editing.
	ReadOnly i18n.Msg
	// Text is the editor's content. Secrets are never in it.
	Text string
	// Unreadable is true when the file cannot be read as a JSON object, so
	// its text is not shown (it might hold a password).
	Unreadable bool
	Running    bool
	Errors     []Problem
	Error      i18n.Msg
}

func (SettingsRawPage) templateName() string { return "settingsraw" }
func (SettingsRawPage) navItem() string      { return PathWorlds }

func (p SettingsRawPage) hasErrors() bool { return len(p.Errors) > 0 || !p.Error.IsZero() }

// Problems lists the errors for the summary.
func (p SettingsRawPage) Problems() []Problem { return p.Errors }

func (p SettingsRawPage) WorldPath() string { return worldPath(p.WorldID) }
func (p SettingsRawPage) FormPath() string  { return settingsPath(p.WorldID) }

// MaxBytes is the largest text the editor accepts.
func (SettingsRawPage) MaxBytes() uint64 { return gamesettings.MaxBytes }
func (SettingsRawPage) File() string     { return gamesettings.FileServerSettings }
func (p SettingsRawPage) Path() string {
	return settingsRawPath(p.WorldID, gamesettings.FileServerSettings)
}

// rawFile checks the file named in the path is one the editor edits.
func (h *settingsHandlers) rawFile(w http.ResponseWriter, r *http.Request, u store.User) bool {
	if r.PathValue("file") != gamesettings.FileServerSettings {
		h.s.notFound(w, r, u)
		return false
	}
	return true
}

func (h *settingsHandlers) rawPage(sw settingsWorld, text []byte) SettingsRawPage {
	p := SettingsRawPage{WorldID: sw.sum.ID, WorldName: sw.sum.Name, ReadOnly: sw.readOnly, Running: sw.running}
	stripped, _ := gamesettings.StripSecrets(text)
	if stripped == nil {
		p.Unreadable = true
		return p
	}
	p.Text = string(stripped)
	return p
}

func (h *settingsHandlers) handleRawForm(w http.ResponseWriter, r *http.Request, u store.User) {
	if !h.rawFile(w, r, u) {
		return
	}
	sw, ok := h.load(w, r, u)
	if !ok {
		return
	}
	text := sw.text()
	if c, ok := h.fromEdit(r.URL.Query().Get("edit"), u, sw.sum.ID, true); ok {
		if f, ok := c.file(world.ServerSettingsFile); ok {
			text = f.New
		}
	}
	h.s.page(w, r, http.StatusOK, u.Username, h.rawPage(sw, text))
}

// handleRawPreview checks the edited text and shows its diff.
func (h *settingsHandlers) handleRawPreview(w http.ResponseWriter, r *http.Request, u store.User) {
	s := h.s
	if !h.rawFile(w, r, u) {
		return
	}
	sw, ok := h.load(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	content := strings.ReplaceAll(r.PostFormValue("content"), "\r\n", "\n")
	if !sw.readOnly.IsZero() {
		p := h.rawPage(sw, sw.text())
		p.Error = sw.readOnly
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	if errs := gamesettings.Validate(gamesettings.FileServerSettings, sw.line, []byte(content)); len(errs) > 0 {
		p := h.rawPage(sw, sw.text())
		p.Text, p.Unreadable = content, false
		p.Errors = rawProblems(errs, content)
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	next := gamesettings.MergeSecrets([]byte(content), sw.cfg.Data)
	change := pendingChange{UserID: u.ID, WorldID: sw.sum.ID, Raw: true}
	if len(gamesettings.Diff(sw.cfg.Data, next)) > 0 {
		change.Files = append(change.Files, pendingFile{File: world.ServerSettingsFile, Hash: sw.cfg.Hash, New: next})
	}
	h.showPreview(w, r, u, sw, change, false)
}

// rawProblems words each error of a raw edit, naming its line and, where it
// helps, quoting it. A line under a secret key is never quoted.
func rawProblems(errs []gamesettings.FieldError, content string) []Problem {
	lines := strings.Split(content, "\n")
	out := make([]Problem, 0, len(errs))
	for _, fe := range errs {
		msg := rawProblem(fe)
		if fe.Line > 0 && fe.Line <= len(lines) && fe.Code != gamesettings.CodeTooLarge && !isSecretPath(fe.Key) {
			if text := echoLine(lines[fe.Line-1]); text != "" {
				msg = i18n.M("settings.err.with_line", "problem", msg, "text", text)
			}
		}
		out = append(out, Problem{FieldID: "content", Message: msg})
	}
	return out
}

func isSecretPath(key string) bool {
	top := key
	if i := strings.IndexAny(key, ".["); i >= 0 {
		top = key[:i]
	}
	for _, s := range gamesettings.SecretKeys {
		if s == top {
			return true
		}
	}
	return false
}

func echoLine(line string) string {
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) > maxLineEcho {
		r := []rune(line)
		line = string(r[:maxLineEcho]) + "…"
	}
	return line
}

func rawProblem(fe gamesettings.FieldError) i18n.Msg {
	switch fe.Code {
	case gamesettings.CodeSyntax:
		return i18n.M("settings.err.syntax", "line", fe.Line)
	case gamesettings.CodeNotObject:
		return i18n.M("settings.err.not_object", "line", fe.Line)
	case gamesettings.CodeTooLarge:
		return i18n.M("settings.err.too_large", "size", i18n.Size(gamesettings.MaxBytes))
	case gamesettings.CodeWrongType:
		return i18n.M("settings.err.wrong_type", "line", fe.Line, "key", fe.Key, "want", typeList(strings.Split(fe.Want, " or ")))
	case gamesettings.CodeOutOfRange:
		return rangeProblem(fe)
	case gamesettings.CodeMissing:
		return i18n.M("settings.err.missing", "line", fe.Line, "key", fe.Key)
	}
	return i18n.M("settings.err.other", "line", fe.Line, "key", fe.Key)
}

func rangeProblem(fe gamesettings.FieldError) i18n.Msg {
	if strings.Contains(fe.Want, "|") {
		return i18n.M("settings.err.enum", "line", fe.Line, "key", fe.Key, "values", strings.ReplaceAll(fe.Want, "|", ", "))
	}
	lo, hi, ok := strings.Cut(fe.Want, "..")
	switch {
	case !ok:
		return i18n.M("settings.err.other", "line", fe.Line, "key", fe.Key)
	case lo != "" && hi != "":
		return i18n.M("settings.err.range", "line", fe.Line, "key", fe.Key, "lo", lo, "hi", hi)
	case lo != "":
		return i18n.M("settings.err.range_min", "line", fe.Line, "key", fe.Key, "lo", lo)
	}
	return i18n.M("settings.err.range_max", "line", fe.Line, "key", fe.Key, "hi", hi)
}

// typeList is the allowed kinds of a value as one phrase, "a or b or c".
func typeList(names []string) any {
	if len(names) == 0 {
		return ""
	}
	first := typeName(names[0])
	if len(names) == 1 {
		return first
	}
	return i18n.M("settings.type.either", "a", first, "b", typeList(names[1:]))
}

func typeName(n string) any {
	switch n {
	case "boolean":
		return i18n.M("settings.type.boolean")
	case "string":
		return i18n.M("settings.type.string")
	case "integer":
		return i18n.M("settings.type.integer")
	case "number":
		return i18n.M("settings.type.number")
	case "array":
		return i18n.M("settings.type.array")
	case "object":
		return i18n.M("settings.type.object")
	case "null":
		return i18n.M("settings.type.null")
	}
	return strconv.Quote(n)
}
