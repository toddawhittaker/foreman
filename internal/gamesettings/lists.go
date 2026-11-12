package gamesettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxReasonChars is the longest kick or ban reason, in characters
// (SPEC 8.2).
const MaxReasonChars = 200

// ValidateReason refuses a kick or ban reason that is not one line of at
// most MaxReasonChars characters: it must be UTF-8 and hold no control
// character or line or paragraph separator, so it cannot end the console
// command it is sent in (SPEC 14.9), as names cannot. An empty reason passes.
func ValidateReason(reason string) error {
	switch {
	case !utf8.ValidString(reason):
		return errors.New("reason is not valid UTF-8")
	case utf8.RuneCountInString(reason) > MaxReasonChars:
		return fmt.Errorf("reason is longer than %d characters", MaxReasonChars)
	case strings.IndexFunc(reason, EndsCommand) >= 0:
		return errors.New("reason is more than one line or holds a control character")
	}
	return nil
}

// parseNames reads a JSON array of player names, the format of the admin
// list and the whitelist. Names are compared without regard to case, as the
// game does, so a later repeat is dropped. A file that is not an array of
// valid names is refused whole rather than partly read.
func parseNames(b []byte, what string) ([]string, error) {
	raw, err := parseArray(b, what)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	names := []string{}
	for _, e := range raw {
		var n string
		if err := json.Unmarshal(e, &n); err != nil {
			return nil, fmt.Errorf("%s is not an array of names", what)
		}
		if err := ValidateAdminName(n); err != nil {
			return nil, err
		}
		if k := strings.ToLower(n); !seen[k] {
			seen[k] = true
			names = append(names, n)
		}
	}
	return names, nil
}

func parseArray(b []byte, what string) ([]json.RawMessage, error) {
	if len(b) > MaxBytes {
		return nil, fmt.Errorf("%s is too large", what)
	}
	if t := bytes.TrimSpace(b); len(t) == 0 || t[0] != '[' {
		return nil, fmt.Errorf("%s is not an array", what)
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s is not an array", what)
	}
	return raw, nil
}

// ParseWhitelist reads server-whitelist.json, an array of player names, as
// ParseAdminList reads the admin list.
func ParseWhitelist(b []byte) ([]string, error) { return parseNames(b, "whitelist") }

// NameListJSON is the text of an admin list or whitelist holding names.
func NameListJSON(names []string) []byte {
	if len(names) == 0 {
		return []byte("[]\n")
	}
	return marshalList(names)
}

// Ban is one entry of the ban list. Reason is empty when none was given.
type Ban struct {
	Name, Reason string
}

// banObject is how the game writes a ban that has a reason.
type banObject struct {
	Username string `json:"username"`
	Reason   string `json:"reason"`
}

// ParseBanList reads server-banlist.json: an array whose entries are a name,
// or an object with "username" and "reason" (the game writes the object only
// when a reason was given). A repeated name, compared without regard to
// case, is dropped. A file that does not hold valid names is refused whole.
// A reason is kept as written: it is shown, and checked again before it is
// ever sent.
func ParseBanList(b []byte) ([]Ban, error) {
	raw, err := parseArray(b, "ban list")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	bans := []Ban{}
	for _, e := range raw {
		var ban Ban
		var obj banObject
		if json.Unmarshal(e, &ban.Name) != nil {
			if t := bytes.TrimSpace(e); len(t) == 0 || t[0] != '{' || json.Unmarshal(e, &obj) != nil {
				return nil, errors.New("ban list entry is not a name or a ban")
			}
			ban = Ban{Name: obj.Username, Reason: obj.Reason}
		}
		if err := ValidateAdminName(ban.Name); err != nil {
			return nil, err
		}
		if k := strings.ToLower(ban.Name); !seen[k] {
			seen[k] = true
			bans = append(bans, ban)
		}
	}
	return bans, nil
}

// BanNames lists the names of bans, in order.
func BanNames(bans []Ban) []string {
	out := make([]string, len(bans))
	for i, b := range bans {
		out[i] = b.Name
	}
	return out
}

// BanListJSON is the text of server-banlist.json for bans, in the game's own
// format: a bare name when there is no reason, an object when there is.
func BanListJSON(bans []Ban) []byte {
	if len(bans) == 0 {
		return []byte("[]\n")
	}
	entries := make([]any, len(bans))
	for i, b := range bans {
		if b.Reason == "" {
			entries[i] = b.Name
		} else {
			entries[i] = banObject{Username: b.Name, Reason: b.Reason}
		}
	}
	return marshalList(entries)
}

// marshalList writes v indented, without escaping markup characters, so the
// file stays readable.
func marshalList(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v) // names and reasons are strings, which always encode
	return buf.Bytes()
}
