package notify

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// BodyVersion is the version of the generic JSON body. A change that is not
// a new optional field raises it.
const BodyVersion = 1

// discordMaxContent is the longest message Discord accepts. discordMaxName
// bounds each name before it is escaped, which at most doubles it, so the
// longest message stays well under the limit and is never cut.
const (
	discordMaxContent = 2000
	discordMaxName    = 400
)

// Text is the message for m in loc's language. Names from the game and the
// world's name are sent as written; esc marks up anything the target would
// read as formatting.
func Text(loc i18n.Locale, m Message, esc func(string) string) string {
	world, player := esc(m.World.Name), esc(m.Player)
	switch m.Event {
	case Start:
		return loc.T("notify.msg.start", "world", world)
	case Stop:
		return loc.T("notify.msg.stop", "world", world)
	case Crash:
		return loc.T("notify.msg.crash", "world", world)
	case CrashLoop:
		return loc.T("notify.msg.crash_loop", "world", world)
	case PlayerJoin:
		return loc.T("notify.msg.player_join", "world", world, "player", player)
	case PlayerLeave:
		return loc.T("notify.msg.player_leave", "world", world, "player", player)
	case ScheduledRestart:
		if m.Skipped {
			return loc.T("notify.msg.restart_skipped", "world", world)
		}
		return loc.T("notify.msg.restart_ran", "world", world)
	case LowDisk:
		return loc.T("notify.msg.low_disk", "world", world, "free", loc.Bytes(m.FreeBytes))
	case TokenExpiring:
		return loc.T("notify.msg.token_expiring", "token", esc(m.Token.Name), "owner", esc(m.Token.Owner),
			"expires", m.Token.Expires.UTC().Format(TimeLayout))
	}
	return ""
}

// TimeLayout writes a time to the minute in UTC, as the pages and the
// messages here both write it, so a time reads the same in every language.
const TimeLayout = "2006-01-02 15:04 UTC"

func plain(s string) string { return s }

// discordName shortens a name, then escapes it, so an escape is never split.
func discordName(s string) string {
	if r := []rune(s); len(r) > discordMaxName {
		s = string(r[:discordMaxName-1]) + "…"
	}
	return discordEscaper.Replace(s)
}

// discordEscaper puts a backslash before each character Discord reads as
// markdown, so a world or player name cannot format the message.
var discordEscaper = strings.NewReplacer(
	`\`, `\\`, `*`, `\*`, `_`, `\_`, "~", `\~`, "`", "\\`", `|`, `\|`, `>`, `\>`,
	`#`, `\#`, `[`, `\[`, `]`, `\]`, `(`, `\(`, `)`, `\)`, `-`, `\-`, `<`, `\<`, `@`, `\@`,
)

// Pushover's limits on a message and its title, in characters.
const (
	pushoverMaxMessage = 1024
	pushoverMaxTitle   = 250
)

// truncate keeps at most n characters of s, cutting on a character.
func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// highPriority is the events ntfy and Pushover mark urgent (SPEC 8.11).
func highPriority(e Event) bool { return e == Crash || e == CrashLoop || e == LowDisk }

// pushoverBody is the form Pushover takes. Priority is 1 or 0, never 2,
// which keeps alerting until someone acknowledges it.
func pushoverBody(loc i18n.Locale, m Message, token, user string) []byte {
	priority := 0
	if highPriority(m.Event) {
		priority = 1
	}
	return pushoverForm(token, user, Text(loc, m, plain), "Foreman", priority)
}

// pushoverForm encodes the form, cutting the message and title to
// Pushover's limits.
func pushoverForm(token, user, message, title string, priority int) []byte {
	return []byte(url.Values{
		"token":    {token},
		"user":     {user},
		"message":  {truncate(message, pushoverMaxMessage)},
		"title":    {truncate(title, pushoverMaxTitle)},
		"priority": {strconv.Itoa(priority)},
	}.Encode())
}

// request is what one delivery posts, and where.
type request struct {
	url         string
	contentType string
	headers     map[string]string
	body        []byte
}

// genericBody is the documented generic JSON body (SPEC 8.11).
type genericBody struct {
	Version   int           `json:"version"`
	Event     Event         `json:"event"`
	Time      string        `json:"time"`
	World     genericWorld  `json:"world"`
	Text      string        `json:"text"`
	Player    string        `json:"player,omitempty"`
	Reason    string        `json:"reason,omitempty"`
	Skipped   *bool         `json:"skipped,omitempty"`
	FreeBytes *uint64       `json:"free_bytes,omitempty"`
	Token     *genericToken `json:"token,omitempty"`
}

type genericToken struct {
	Name    string `json:"name"`
	Owner   string `json:"owner"`
	Expires string `json:"expires"`
}

type genericWorld struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// discordBody turns off every mention, so no name can ping a channel.
type discordBody struct {
	Content         string          `json:"content"`
	Username        string          `json:"username"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}

// build makes the request for m to a webhook of preset p, whose opened
// address is raw, in loc's language.
func build(p Preset, raw string, loc i18n.Locale, m Message) (request, error) {
	switch p {
	case Discord:
		text := Text(loc, m, discordName)
		b, err := json.Marshal(discordBody{Content: text, Username: "Foreman", AllowedMentions: allowedMentions{Parse: []string{}}})
		return request{url: raw, contentType: "application/json", body: b}, err
	case Ntfy:
		h := map[string]string{"Title": "Foreman"}
		if highPriority(m.Event) {
			h["Priority"] = "high"
		}
		return request{url: raw, contentType: "text/plain; charset=utf-8", headers: h, body: []byte(Text(loc, m, plain))}, nil
	case Pushover:
		endpoint, token, user := pushoverKeys(raw)
		return request{url: endpoint, contentType: "application/x-www-form-urlencoded", body: pushoverBody(loc, m, token, user)}, nil
	default:
		b, err := json.Marshal(generic(loc, m))
		return request{url: raw, contentType: "application/json", body: b}, err
	}
}

func generic(loc i18n.Locale, m Message) genericBody {
	g := genericBody{
		Version: BodyVersion,
		Event:   m.Event,
		Time:    m.Time.UTC().Format(time.RFC3339),
		World:   genericWorld{ID: m.World.ID, Name: m.World.Name},
		Text:    Text(loc, m, plain),
		Reason:  m.Reason,
	}
	switch m.Event {
	case PlayerJoin, PlayerLeave:
		g.Player = m.Player
	case ScheduledRestart:
		g.Skipped = &m.Skipped
	case LowDisk:
		g.FreeBytes = &m.FreeBytes
	case TokenExpiring:
		g.Token = &genericToken{Name: m.Token.Name, Owner: m.Token.Owner, Expires: m.Token.Expires.UTC().Format(time.RFC3339)}
	}
	return g
}
