package notify

import (
	"encoding/json"
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
	}
	return ""
}

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

// request is what one delivery posts.
type request struct {
	contentType string
	headers     map[string]string
	body        []byte
}

// genericBody is the documented generic JSON body (SPEC 8.11).
type genericBody struct {
	Version   int          `json:"version"`
	Event     Event        `json:"event"`
	Time      string       `json:"time"`
	World     genericWorld `json:"world"`
	Text      string       `json:"text"`
	Player    string       `json:"player,omitempty"`
	Reason    string       `json:"reason,omitempty"`
	Skipped   *bool        `json:"skipped,omitempty"`
	FreeBytes *uint64      `json:"free_bytes,omitempty"`
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

// build makes the request for m to a webhook of preset p in loc's language.
func build(p Preset, loc i18n.Locale, m Message) (request, error) {
	switch p {
	case Discord:
		text := Text(loc, m, discordName)
		b, err := json.Marshal(discordBody{Content: text, Username: "Foreman", AllowedMentions: allowedMentions{Parse: []string{}}})
		return request{contentType: "application/json", body: b}, err
	case Ntfy:
		h := map[string]string{"Title": "Foreman"}
		if m.Event == Crash || m.Event == CrashLoop || m.Event == LowDisk {
			h["Priority"] = "high"
		}
		return request{contentType: "text/plain; charset=utf-8", headers: h, body: []byte(Text(loc, m, plain))}, nil
	default:
		b, err := json.Marshal(generic(loc, m))
		return request{contentType: "application/json", body: b}, err
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
	}
	return g
}
