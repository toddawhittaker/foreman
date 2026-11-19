package notify

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// Preset is the kind of service a webhook posts to (SPEC 8.11).
type Preset string

// The presets. Discord is the default.
const (
	Discord  Preset = "discord"
	Ntfy     Preset = "ntfy"
	Generic  Preset = "generic"
	Pushover Preset = "pushover"
)

// Presets lists the presets in the order the page shows them.
var Presets = []Preset{Discord, Ntfy, Pushover, Generic}

// Known reports whether p is one of Presets.
func (p Preset) Known() bool { return p == Discord || p == Ntfy || p == Generic || p == Pushover }

// MaxURLLength bounds a webhook address.
const MaxURLLength = 2048

// The reasons CheckURL refuses an address. None of them holds the address.
var (
	ErrURLMissing    = errors.New("no webhook address was given")
	ErrURLTooLong    = errors.New("the webhook address is too long")
	ErrURLInvalid    = errors.New("the webhook address is not a valid web address")
	ErrURLScheme     = errors.New("the webhook address does not start with https:// or http://")
	ErrURLHasUser    = errors.New("the webhook address holds a user name or password")
	ErrURLNotDiscord = errors.New("the webhook address is not a Discord webhook address")
	ErrURLNoTopic    = errors.New("the webhook address names no ntfy topic")
	ErrKeyMissing    = errors.New("a Pushover key was not given")
	ErrKeyInvalid    = errors.New("a Pushover key holds a character other than a letter or digit, or is too long")
)

// Pushover's endpoint is fixed, so the admin gives only the two keys.
const (
	PushoverEndpoint = "https://api.pushover.net/1/messages.json"
	PushoverHost     = "api.pushover.net"
	// MaxKeyLength bounds a Pushover key; Pushover's are 30 characters.
	MaxKeyLength = 64
)

// CheckKey checks one Pushover key. The error never holds the key.
func CheckKey(key string) error {
	if key == "" {
		return ErrKeyMissing
	}
	if len(key) > MaxKeyLength {
		return ErrKeyInvalid
	}
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return ErrKeyInvalid
		}
	}
	return nil
}

// pushoverAddress is what a Pushover webhook keeps sealed in its address:
// the endpoint with the two keys in its query. Only send reads the keys
// back, and it posts them in the body, never in the address.
func pushoverAddress(token, user string) string {
	return PushoverEndpoint + "?" + url.Values{"token": {token}, "user": {user}}.Encode()
}

// pushoverKeys splits a stored Pushover address into the address to post
// to and the two keys.
func pushoverKeys(raw string) (endpoint, token, user string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", ""
	}
	q := u.Query()
	u.RawQuery = ""
	return u.String(), q.Get("token"), q.Get("user")
}

// discordHosts are the hosts Discord gives webhook addresses on.
var discordHosts = map[string]bool{
	"discord.com": true, "discordapp.com": true, "ptb.discord.com": true, "canary.discord.com": true,
}

// CheckURL checks a webhook address for preset p and returns its host, the
// one part of it a page may show (SPEC 14.8). The error never holds the
// address.
func CheckURL(p Preset, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return "", ErrURLMissing
	case len(raw) > MaxURLLength:
		return "", ErrURLTooLong
	case strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return "", ErrURLInvalid
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", ErrURLInvalid
	}
	// http is allowed, and the page warns that it is unencrypted.
	if !strings.EqualFold(u.Scheme, "https") && !strings.EqualFold(u.Scheme, "http") {
		return "", ErrURLScheme
	}
	if u.User != nil {
		return "", ErrURLHasUser
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || u.Opaque != "" {
		return "", ErrURLInvalid
	}
	switch p {
	case Discord:
		if !discordHosts[host] || !strings.HasPrefix(u.Path, "/api/webhooks/") {
			return "", ErrURLNotDiscord
		}
	case Ntfy:
		if strings.Trim(u.Path, "/") == "" {
			return "", ErrURLNoTopic
		}
	}
	return host, nil
}
