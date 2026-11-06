package web

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// maxWhole is the largest whole number the game stores in a setting the form
// edits (gamesettings validates the same range).
const maxWhole = 4294967295

var (
	// jsonWhole is a whole number written the way JSON writes one.
	jsonWhole  = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	typedWhole = regexp.MustCompile(`^[0-9]+$`)
)

// serverValues are the settings the form shows, read from the text of
// server-settings.json. A key the file lacks, or holds in another shape, reads
// as empty, so the form changes nothing it did not show (see applyForm).
type serverValues struct {
	Name, Description string
	MaxPlayers        string
	Interval, Slots   string
	Verify, AutoPause bool
	Visibility        gamesettings.Visibility // "" when the file does not say
	PasswordSet       bool
}

// readServerValues reads the form's fields from b, which must be one JSON
// object. A repeated key reads as its last value, as the game does.
func readServerValues(b []byte) (serverValues, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return serverValues{}, false
	}
	str := func(key string) string {
		var s string
		if json.Unmarshal(raw[key], &s) != nil {
			return ""
		}
		return s
	}
	flag := func(key string) bool {
		var v bool
		return json.Unmarshal(raw[key], &v) == nil && v
	}
	whole := func(key string) string {
		s := strings.TrimSpace(string(raw[key]))
		if jsonWhole.MatchString(s) {
			return s
		}
		return ""
	}
	v := serverValues{
		Name: str("name"), Description: str("description"),
		MaxPlayers: whole("max_players"), Interval: whole("autosave_interval"), Slots: whole("autosave_slots"),
		Verify: flag("require_user_verification"), AutoPause: flag("auto_pause"),
	}
	if vis, ok := gamesettings.ReadVisibility(b); ok {
		v.Visibility = vis
	}
	_, set := gamesettings.StripSecrets(b)
	v.PasswordSet = set["game_password"]
	return v, true
}

// postedForm is the settings form as submitted.
type postedForm struct {
	Name, Description string
	Visibility        string
	Password          string
	RemovePassword    bool
	MaxPlayers        string
	Interval, Slots   string
	Verify, AutoPause bool
	Admins            string
	AdminsPosted      bool
}

func readPostedForm(get func(string) string, has func(string) bool) postedForm {
	return postedForm{
		Name: get("name"), Description: strings.ReplaceAll(get("description"), "\r\n", "\n"),
		Visibility: get("visibility"), Password: get("game_password"), RemovePassword: has("game_password_remove"),
		MaxPlayers: get("max_players"), Interval: get("autosave_interval"), Slots: get("autosave_slots"),
		Verify: has("require_user_verification"), AutoPause: has("auto_pause"),
		Admins: strings.ReplaceAll(get("admins"), "\r\n", "\n"), AdminsPosted: has("admins"),
	}
}

// formErrors are the form's field errors.
type formErrors struct {
	NameError, PasswordError, MaxPlayersError, IntervalError, SlotsError, AdminsError i18n.Msg
}

func (e formErrors) has() bool {
	return !e.NameError.IsZero() || !e.PasswordError.IsZero() || !e.MaxPlayersError.IsZero() ||
		!e.IntervalError.IsZero() || !e.SlotsError.IsZero() || !e.AdminsError.IsZero()
}

func wholeError() i18n.Msg { return i18n.M("settings.error.whole", "max", maxWhole) }

// parseWhole reads a number field. same is true when the field holds what the
// file holds, so nothing is written for it. An empty field is no change when
// the file has no number there either.
func parseWhole(posted, current string) (canonical string, same bool, problem i18n.Msg) {
	s := strings.TrimSpace(posted)
	if s == "" {
		if current == "" {
			return "", true, i18n.Msg{}
		}
		return "", false, wholeError()
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || !typedWhole.MatchString(s) {
		return "", false, wholeError()
	}
	canonical = strconv.FormatUint(n, 10)
	return canonical, canonical == current, i18n.Msg{}
}

// jsonString is s as a JSON string, without escaping markup characters, so
// the file stays readable.
func jsonString(s string) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func jsonBool(v bool) []byte { return []byte(strconv.FormatBool(v)) }

// applyForm works out the new text of server-settings.json from the posted
// form. Only a field that differs from what the file holds is written, so a
// key the form could not show stays as it is. carried is the game password
// raw JSON from an earlier, previewed edit, kept when the form leaves the
// password alone.
func applyForm(old []byte, cur serverValues, in postedForm, carried []byte) ([]byte, formErrors, error) {
	var errs formErrors
	var fields []gamesettings.Field
	set := func(key string, raw []byte) { fields = append(fields, gamesettings.Field{Key: key, JSON: raw}) }

	// The game refuses a file without name, description and visibility, so a
	// file that lacks one gets it whether or not the field changed (SPEC 11.2).
	name := strings.TrimSpace(in.Name)
	if name == "" {
		errs.NameError = i18n.M("settings.name.empty")
	} else if name != cur.Name || !gamesettings.HasKey(old, "name") {
		set("name", jsonString(name))
	}
	if in.Description != cur.Description || !gamesettings.HasKey(old, "description") {
		set("description", jsonString(in.Description))
	}
	numbers := []struct {
		key, posted, current string
		problem              *i18n.Msg
	}{
		{"max_players", in.MaxPlayers, cur.MaxPlayers, &errs.MaxPlayersError},
		{"autosave_interval", in.Interval, cur.Interval, &errs.IntervalError},
		{"autosave_slots", in.Slots, cur.Slots, &errs.SlotsError},
	}
	for _, n := range numbers {
		canonical, same, problem := parseWhole(n.posted, n.current)
		switch {
		case !problem.IsZero():
			*n.problem = problem
		case !same:
			set(n.key, []byte(canonical))
		}
	}
	if in.Verify != cur.Verify {
		set("require_user_verification", jsonBool(in.Verify))
	}
	if in.AutoPause != cur.AutoPause {
		set("auto_pause", jsonBool(in.AutoPause))
	}
	switch {
	case in.Password != "" && in.RemovePassword:
		errs.PasswordError = i18n.M("settings.password.both")
	case in.Password != "":
		set("game_password", jsonString(in.Password))
	case in.RemovePassword && cur.PasswordSet:
		set("game_password", []byte(`""`))
	case carried != nil:
		set("game_password", carried)
	}
	if errs.has() {
		return nil, errs, nil
	}
	out, err := gamesettings.SetFields(old, fields)
	if err != nil {
		return nil, errs, err
	}
	v := gamesettings.Visibility(in.Visibility)
	if !slices.Contains(gamesettings.FormVisibilities, v) {
		v = ""
	}
	write := v != "" && v != cur.Visibility
	if _, ok := gamesettings.ReadVisibility(out); !ok {
		write = true
		if v == "" {
			v = gamesettings.VisibilityHidden
		}
	}
	if write {
		out, err = gamesettings.SetVisibility(out, v)
		if err != nil {
			return nil, errs, err
		}
	}
	return out, errs, nil
}

// carriedPassword is the game_password value of an earlier edit's text, as
// raw JSON, or nil when that text has no game_password key. An empty value
// is returned as it is, so a removal carries forward.
func carriedPassword(b []byte) []byte {
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	v, ok := raw["game_password"]
	if !ok {
		return nil
	}
	return v
}

// adminLines reads the admins box: one name per line, blank lines dropped,
// each name trimmed, a repeat (ignoring case) dropped. bad is the 1-based
// line of the first name the game could not be sent, or 0.
func adminLines(text string) (names []string, bad int) {
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		n := strings.TrimSpace(line)
		if n == "" {
			continue
		}
		if gamesettings.ValidateAdminName(n) != nil {
			return nil, i + 1
		}
		if k := strings.ToLower(n); !seen[k] {
			seen[k] = true
			names = append(names, n)
		}
	}
	return names, 0
}

// adminListJSON is the text of server-adminlist.json for names.
func adminListJSON(names []string) []byte {
	if len(names) == 0 {
		return []byte("[]\n")
	}
	b, _ := json.MarshalIndent(names, "", "  ") // strings always encode
	return append(b, '\n')
}

// SettingsPage is the server-settings form of a world (SPEC 11.1, DESIGN
// 8.10).
type SettingsPage struct {
	WorldID, WorldName string
	// ReadOnly says why nothing can be changed; zero when the world is open
	// to editing.
	ReadOnly i18n.Msg
	// Running is true when the game runs, so a change to the settings needs a
	// restart.
	Running bool
	// Broken is true when server-settings.json cannot be read as a JSON
	// object, so the form has nothing to show.
	Broken bool
	// Edit is the token of the previewed change the form was refilled from.
	Edit string

	Name, Description string
	// Visibility is "lan", "hidden", or "" when neither is chosen.
	Visibility string
	// VisibilityHint explains an unset or public visibility.
	VisibilityHint    i18n.Msg
	PasswordSet       bool
	MaxPlayers        string
	Interval, Slots   string
	Verify, AutoPause bool
	Admins            string
	AdminsBroken      bool

	formErrors
	Error i18n.Msg
	Done  i18n.Msg
	// DoneSnap is the snapshot the save took; DoneRestart offers Restart.
	DoneSnap    string
	DoneRestart bool
}

func (SettingsPage) templateName() string { return "settings" }
func (SettingsPage) navItem() string      { return PathWorlds }

func (p SettingsPage) hasErrors() bool { return p.has() || !p.Error.IsZero() }

func (p SettingsPage) WorldPath() string { return worldPath(p.WorldID) }
func (p SettingsPage) Path() string      { return settingsPath(p.WorldID) }
func (p SettingsPage) RawPath() string {
	return settingsRawPath(p.WorldID, gamesettings.FileServerSettings)
}
func (p SettingsPage) SnapshotsPath() string { return snapshotsPath(p.WorldID) }
func (p SettingsPage) RestartPath() string   { return gamePath(p.WorldID, "restart") }
func (SettingsPage) File() string            { return gamesettings.FileServerSettings }
func (SettingsPage) AdminFile() string       { return world.ServerAdminListFile }

// Editable is true when the form can be used.
func (p SettingsPage) Editable() bool { return p.ReadOnly.IsZero() && !p.Broken }

// Problems lists the field errors in the order the fields appear.
func (p SettingsPage) Problems() []Problem {
	return problems(
		Problem{"name", p.NameError},
		Problem{"game-password", p.PasswordError},
		Problem{"max-players", p.MaxPlayersError},
		Problem{"autosave-interval", p.IntervalError},
		Problem{"autosave-slots", p.SlotsError},
		Problem{"admins", p.AdminsError},
	)
}
