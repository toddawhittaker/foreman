package web

import (
	"bytes"
	"encoding/json"
	"net/url"
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
// server-settings.json. A whole number the file lacks reads as empty and the
// form shows the game's default as its placeholder. A true-or-false key the
// file lacks reads as the game's default, as does allow_commands. So the
// form changes nothing it did not show (see applyForm).
type serverValues struct {
	Name, Description                                                         string
	MaxPlayers                                                                string
	Interval, Slots                                                           string
	AFK                                                                       string
	Upload, UploadSlots                                                       string
	Verify, IgnoreLimit, AutoPause, PauseOnJoin, OnlyAdminsPause, NonBlocking bool
	// Commands is the allow_commands choice: commandsEveryone,
	// commandsAdmins or commandsNobody.
	Commands    string
	Visibility  gamesettings.Visibility // "" when the file does not say
	PasswordSet bool
}

// The allow_commands choices the form offers, as posted and as compared.
const (
	commandsEveryone = "true"
	commandsAdmins   = "admins-only"
	commandsNobody   = "false"
)

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
	// A missing key reads as the game's own default (SPEC 11.1).
	flag := func(key string) bool {
		if _, ok := raw[key]; !ok {
			return gamesettings.GameDefaultFlag(key)
		}
		var v bool
		return json.Unmarshal(raw[key], &v) == nil && v
	}
	// The game takes true, false and admins-only, as text or, for the first
	// two, as a boolean. Anything else reads as the default, so the form
	// writes nothing it did not show.
	commands := func() string {
		var b bool
		if json.Unmarshal(raw["allow_commands"], &b) == nil {
			return strconv.FormatBool(b)
		}
		var s string
		if json.Unmarshal(raw["allow_commands"], &s) == nil && slices.Contains([]string{commandsEveryone, commandsAdmins, commandsNobody}, s) {
			return s
		}
		return commandsAdmins
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
		AFK: whole("afk_autokick_interval"), Upload: whole("max_upload_in_kilobytes_per_second"), UploadSlots: whole("max_upload_slots"),
		Verify: flag("require_user_verification"), IgnoreLimit: flag("ignore_player_limit_for_returning_players"),
		AutoPause: flag("auto_pause"), PauseOnJoin: flag("auto_pause_when_players_connect"),
		OnlyAdminsPause: flag("only_admins_can_pause_the_game"), NonBlocking: flag("non_blocking_saving"),
		Commands: commands(),
	}
	if vis, ok := gamesettings.ReadVisibility(b); ok {
		v.Visibility = vis
	}
	_, set := gamesettings.StripSecrets(b)
	v.PasswordSet = set["game_password"]
	return v, true
}

// formCaps says which fields a world's form offers. A field that is hidden or
// disabled is not posted, so it must not read as unchecked.
type formCaps struct {
	// PauseOnJoin is true on a game line that has auto_pause_when_players_connect.
	PauseOnJoin bool
	// Fork is true unless Docker reports a host that is not Linux, where the
	// game cannot save by forking (SPEC 8.7).
	Fork bool
}

// postedForm is the settings form as submitted.
type postedForm struct {
	Name, Description string
	Visibility        string
	Password          string
	RemovePassword    bool
	MaxPlayers        string
	Interval, Slots   string
	AFK               string
	Upload            string
	UploadSlots       string
	Verify, AutoPause bool
	IgnoreLimit       bool
	PauseOnJoin       bool
	OnlyAdminsPause   bool
	NonBlocking       bool
	Commands          string
	Admins            string
	AdminsPosted      bool
	Bans, Whitelist   string
	BansPosted        bool
	WhitelistPosted   bool
	// MakeAdmin is a connected player the Make admin button chose.
	MakeAdmin string
}

func readPostedForm(get func(string) string, has func(string) bool) postedForm {
	return postedForm{
		Name: get("name"), Description: strings.ReplaceAll(get("description"), "\r\n", "\n"),
		Visibility: get("visibility"), Password: get("game_password"), RemovePassword: has("game_password_remove"),
		MaxPlayers: get("max_players"), Interval: get("autosave_interval"), Slots: get("autosave_slots"),
		AFK: get("afk_autokick_interval"), Upload: get("max_upload_in_kilobytes_per_second"), UploadSlots: get("max_upload_slots"),
		Verify: has("require_user_verification"), AutoPause: has("auto_pause"),
		IgnoreLimit: has("ignore_player_limit_for_returning_players"), PauseOnJoin: has("auto_pause_when_players_connect"),
		OnlyAdminsPause: has("only_admins_can_pause_the_game"), NonBlocking: has("non_blocking_saving"),
		Commands: get("allow_commands"),
		Admins:   strings.ReplaceAll(get("admins"), "\r\n", "\n"), AdminsPosted: has("admins"),
		MakeAdmin: strings.TrimSpace(get("make_admin")),
		Bans:      strings.ReplaceAll(get("bans"), "\r\n", "\n"), BansPosted: has("bans"),
		Whitelist: strings.ReplaceAll(get("whitelist"), "\r\n", "\n"), WhitelistPosted: has("whitelist"),
	}
}

// addMadeAdmin puts the player the Make admin button chose on a new line of
// the admins box, so the name goes through the same checks as a typed one.
func (in *postedForm) addMadeAdmin() {
	if in.MakeAdmin == "" {
		return
	}
	if rest := strings.TrimRight(in.Admins, " \t\r\n"); rest != "" {
		in.Admins = rest + "\n" + in.MakeAdmin
	} else {
		in.Admins = in.MakeAdmin
	}
	in.AdminsPosted = true
}

// connected lists the players in names who are not on the admins box's lines
// and whose names the game could be sent, without regard to case.
func connected(names []string, adminsText string) []string {
	listed := map[string]bool{}
	for _, line := range strings.Split(adminsText, "\n") {
		listed[strings.ToLower(strings.TrimSpace(line))] = true
	}
	var out []string
	for _, n := range names {
		if !listed[strings.ToLower(n)] && gamesettings.ValidateAdminName(n) == nil {
			out = append(out, n)
		}
	}
	return out
}

// formErrors are the form's field errors.
type formErrors struct {
	NameError, PasswordError, MaxPlayersError, IntervalError, SlotsError, AdminsError i18n.Msg
	AFKError, UploadError, UploadSlotsError                                           i18n.Msg
	BansError, WhitelistError                                                         i18n.Msg
}

func (e formErrors) has() bool {
	return !e.NameError.IsZero() || !e.PasswordError.IsZero() || !e.MaxPlayersError.IsZero() ||
		!e.IntervalError.IsZero() || !e.SlotsError.IsZero() || !e.AdminsError.IsZero() ||
		!e.AFKError.IsZero() || !e.UploadError.IsZero() || !e.UploadSlotsError.IsZero() ||
		!e.BansError.IsZero() || !e.WhitelistError.IsZero()
}

func wholeError() i18n.Msg { return i18n.M("settings.error.whole", "max", maxWhole) }

// parseWhole reads a number field. same is true when the field holds what the
// file holds, so nothing is written for it. An empty field is no change when
// the file has no number there either; when it has one, the empty field asks
// for the key to go, and canonical is "" with same false.
func parseWhole(posted, current string) (canonical string, same bool, problem i18n.Msg) {
	s := strings.TrimSpace(posted)
	if s == "" {
		return "", current == "", i18n.Msg{}
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
// password alone. caps says which fields this world's form offers; a field it
// does not offer is never written, whatever was posted.
func applyForm(old []byte, cur serverValues, in postedForm, carried []byte, caps formCaps) ([]byte, formErrors, error) {
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
		{"afk_autokick_interval", in.AFK, cur.AFK, &errs.AFKError},
		{"max_upload_in_kilobytes_per_second", in.Upload, cur.Upload, &errs.UploadError},
		{"max_upload_slots", in.UploadSlots, cur.UploadSlots, &errs.UploadSlotsError},
	}
	for _, n := range numbers {
		canonical, same, problem := parseWhole(n.posted, n.current)
		switch {
		case !problem.IsZero():
			*n.problem = problem
		case !same && canonical == "":
			// Cleared: the game uses its own default again.
			set(n.key, nil)
		case !same:
			set(n.key, []byte(canonical))
		}
	}
	flags := []struct {
		key             string
		posted, current bool
		offered         bool
	}{
		{"require_user_verification", in.Verify, cur.Verify, true},
		{"ignore_player_limit_for_returning_players", in.IgnoreLimit, cur.IgnoreLimit, true},
		{"auto_pause", in.AutoPause, cur.AutoPause, true},
		{"auto_pause_when_players_connect", in.PauseOnJoin, cur.PauseOnJoin, caps.PauseOnJoin},
		{"only_admins_can_pause_the_game", in.OnlyAdminsPause, cur.OnlyAdminsPause, true},
		{"non_blocking_saving", in.NonBlocking, cur.NonBlocking, caps.Fork},
	}
	for _, f := range flags {
		if f.offered && f.posted != f.current {
			set(f.key, jsonBool(f.posted))
		}
	}
	if in.Commands != cur.Commands && slices.Contains([]string{commandsEveryone, commandsAdmins, commandsNobody}, in.Commands) {
		if in.Commands == commandsAdmins {
			set("allow_commands", jsonString(commandsAdmins))
		} else {
			set("allow_commands", []byte(in.Commands))
		}
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
func adminListJSON(names []string) []byte { return gamesettings.NameListJSON(names) }

// SettingsPage is the server-settings form of a world (SPEC 11.1, DESIGN
// 8.10).
type SettingsPage struct {
	WorldID, WorldName string
	// Active marks the active world in the heading, as on its Overview.
	Active bool
	Live   LiveState
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
	VisibilityHint i18n.Msg
	// PasswordDropped is true when a form was refused after a password was
	// typed: the page never carries it back, so it must be entered again.
	PasswordDropped bool
	// PasswordPending is true when the form was refilled from a previewed
	// change whose password differs from the one in the file.
	PasswordPending bool
	PasswordSet     bool
	MaxPlayers      string
	Interval, Slots string
	AFK             string
	Upload          string
	UploadSlots     string
	Verify          bool
	IgnoreLimit     bool
	AutoPause       bool
	PauseOnJoin     bool
	OnlyAdminsPause bool
	NonBlocking     bool
	// Commands is the allow_commands choice: "true", "admins-only" or "false".
	Commands string
	// HasPauseOnJoin is true when the world's game line offers the pause while
	// someone joins; 1.1 has no such setting.
	HasPauseOnJoin bool
	// ForkOff is true when Docker reports a host that is not Linux, so fork
	// saving is shown disabled; HostOS is what Docker reported.
	ForkOff      bool
	HostOS       string
	Admins       string
	AdminsBroken bool
	// Connected are the players in the running game who are not admins.
	Connected []string

	// Bans and Whitelist are the lists' boxes, used while Foreman writes the
	// file. BanList and WhitelistNames are the lists for the cards shown
	// while the game keeps them, which LiveBans and LiveWhitelist say.
	Bans, Whitelist             string
	BansBroken, WhitelistBroken bool
	BanList                     []gamesettings.Ban
	WhitelistNames              []string
	LiveBans, LiveWhitelist     bool
	// UseWhitelist is the world's switch; WhitelistRestart is true when the
	// running game was started with it the other way.
	UseWhitelist, WhitelistRestart bool
	// Player is the result of a player action sent from this page.
	Player *PlayerResult

	formErrors
	Error i18n.Msg
	Done  i18n.Msg
	// DoneSnap is the snapshot the save took; DoneRestart offers Restart.
	DoneSnap    string
	DoneRestart bool
}

// Default is the game's own value of a whole-number field, shown as its
// placeholder and in its hint, so an unset field is never an empty unknown.
func (SettingsPage) Default(key string) uint32 { return gamesettings.GameDefaultWhole(key) }

// DefaultOn is the game's own value of a true-or-false field.
func (SettingsPage) DefaultOn(key string) bool { return gamesettings.GameDefaultFlag(key) }

// ForemanSlots is the autosave slots Foreman gives a world it creates.
func (SettingsPage) ForemanSlots() int { return gamesettings.ForemanAutosaveSlots }

// NetworkOpen is true when the collapsed Network group holds a setting the
// file sets or an error, so nothing set is hidden.
func (p SettingsPage) NetworkOpen() bool {
	return p.Upload != "" || p.UploadSlots != "" || !p.UploadError.IsZero() || !p.UploadSlotsError.IsZero()
}

// setPosted shows the form as it was posted, for a refused form.
func (p *SettingsPage) setPosted(in postedForm) {
	p.Name, p.Description, p.MaxPlayers = in.Name, in.Description, in.MaxPlayers
	p.Interval, p.Slots, p.AFK, p.Upload, p.UploadSlots = in.Interval, in.Slots, in.AFK, in.Upload, in.UploadSlots
	p.Verify, p.IgnoreLimit, p.AutoPause, p.PauseOnJoin = in.Verify, in.IgnoreLimit, in.AutoPause, in.PauseOnJoin
	p.OnlyAdminsPause, p.NonBlocking = in.OnlyAdminsPause, in.NonBlocking
	if slices.Contains([]string{commandsEveryone, commandsAdmins, commandsNobody}, in.Commands) {
		p.Commands = in.Commands
	}
}

func (SettingsPage) templateName() string { return "settings" }
func (SettingsPage) navItem() string      { return PathWorlds }

// Tabs are the world's pages, with Settings current (DESIGN 6).
func (p SettingsPage) Tabs() WorldTabs {
	return settingsTabs(p.WorldID, false)
}

func (p SettingsPage) hasErrors() bool { return p.has() || !p.Error.IsZero() }

func (p SettingsPage) WorldPath() string { return worldPath(p.WorldID) }
func (p SettingsPage) Path() string      { return settingsPath(p.WorldID) }
func (p SettingsPage) RawPath() string {
	return settingsRawPath(p.WorldID, gamesettings.FileServerSettings)
}
func (p SettingsPage) SnapshotsPath() string { return snapshotsPath(p.WorldID) }

// RestartPath is the Server page's stop page for a restart, told to come back
// here on Cancel.
func (p SettingsPage) RestartPath() string {
	return gamePath(p.WorldID, "restart") + "?" + fromQuery + "=" + url.QueryEscape(p.Path())
}

func (SettingsPage) File() string          { return gamesettings.FileServerSettings }
func (SettingsPage) AdminFile() string     { return world.ServerAdminListFile }
func (SettingsPage) BanFile() string       { return world.ServerBanListFile }
func (SettingsPage) WhitelistFile() string { return world.ServerWhiteListFile }

// WhitelistPath is where the whitelist switch posts.
func (p SettingsPage) WhitelistPath() string { return settingsWhitelistPath(p.WorldID) }

// PlayerPath is a player action's path on this world: where it posts, and
// for kick and ban the confirmation.
func (p SettingsPage) PlayerPath(action string) string { return playerPath(p.WorldID, action) }

// Editable is true when the form can be used.
func (p SettingsPage) Editable() bool { return p.ReadOnly.IsZero() && !p.Broken }

// Problems lists the field errors in the order the fields appear.
func (p SettingsPage) Problems() []Problem {
	return problems(
		Problem{"name", p.NameError},
		Problem{"game-password", p.PasswordError},
		Problem{"max-players", p.MaxPlayersError},
		Problem{"afk-autokick", p.AFKError},
		Problem{"autosave-interval", p.IntervalError},
		Problem{"autosave-slots", p.SlotsError},
		Problem{"max-upload", p.UploadError},
		Problem{"max-upload-slots", p.UploadSlotsError},
		Problem{"admins", p.AdminsError},
		Problem{"bans", p.BansError},
		Problem{"whitelist", p.WhitelistError},
		Problem{"ban-name", p.BanNameError()},
		Problem{"whitelist-name", p.WhitelistNameError()},
	)
}
