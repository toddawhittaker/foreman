package fakegame

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The game's three player lists. Each is written at once when a command
// changes it, but only if the game was given the matching flag; the
// whitelist is also loaded and enforced only with --use-server-whitelist
// beside --server-whitelist. Without them it lives in memory and is lost
// when the game stops.

// listFlags is where the game was told to keep its lists, as host paths.
type listFlags struct {
	ban, admin string
	whitelist  string // "" when --server-whitelist was not given
	useList    bool   // --use-server-whitelist was given
}

// flagPath finds "flag path" in the container's command and maps the path
// under /factorio to the host. conditional is true when the command passes
// the flag only if that file exists (Foreman's start script does this), and
// the file is missing: then the game did not get the flag.
func flagPath(c dockertest.Container, text, flag string) (host string, conditional bool) {
	m := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(flag) + `\s+([^\s;]+)`).FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	rest, ok := strings.CutPrefix(m[1], "/factorio/")
	src := mountSource(c, "/factorio")
	if !ok || src == "" {
		return "", false
	}
	host = filepath.Join(src, rest)
	if strings.Contains(text, "-f "+m[1]+" ]") {
		if _, err := os.Stat(host); err != nil {
			return "", true
		}
	}
	return host, false
}

// listFlagsFor reads the flags from the container's command. The fake does
// not run the start script's shell; it reads the flags out of its text.
func listFlagsFor(c dockertest.Container) listFlags {
	text := strings.Join(c.Spec.Cmd, " ")
	var f listFlags
	f.ban, _ = flagPath(c, text, "--server-banlist")
	f.admin, _ = flagPath(c, text, "--server-adminlist")
	var skipped bool
	f.whitelist, skipped = flagPath(c, text, "--server-whitelist")
	f.useList = regexp.MustCompile(`(?:^|\s)--use-server-whitelist(?:[\s;]|$)`).MatchString(text) && !skipped
	return f
}

// banEntry is one line of the ban list.
type banEntry struct{ name, reason string }

// loadLists reads the list files the game was given, as it does at start.
func (in *instance) loadLists() {
	in.bans, in.admins, in.whitelist = nil, nil, nil
	if raw := readList(in.flags.ban); raw != nil {
		for _, e := range raw {
			var name string
			var obj struct{ Username, Reason string }
			if json.Unmarshal(e, &name) == nil {
				in.bans = append(in.bans, banEntry{name: name})
			} else if json.Unmarshal(e, &obj) == nil {
				in.bans = append(in.bans, banEntry{name: obj.Username, reason: obj.Reason})
			}
		}
	}
	in.admins = readNames(in.flags.admin)
	if in.flags.useList {
		in.whitelist = readNames(in.flags.whitelist)
	}
}

func readList(path string) []json.RawMessage {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path) //nolint:gosec // the fake's own test folder
	if err != nil {
		return nil
	}
	var out []json.RawMessage
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

func readNames(path string) []string {
	var out []string
	for _, e := range readList(path) {
		var name string
		if json.Unmarshal(e, &name) == nil {
			out = append(out, name)
		}
	}
	return out
}

// saveBans writes the ban list file, if the game was given one.
func (in *instance) saveBans() {
	var entries []string
	for _, b := range in.bans {
		if b.reason == "" {
			entries = append(entries, "  "+quote(b.name))
		} else {
			entries = append(entries, "  \n  {\n    \"username\": "+quote(b.name)+",\n    \"reason\": "+quote(b.reason)+"\n  }")
		}
	}
	in.writeList(in.flags.ban, entries)
}

func (in *instance) saveAdmins() { in.writeList(in.flags.admin, quoteAll(in.admins)) }

// saveWhitelist writes the whitelist only when the game keeps it in a file.
func (in *instance) saveWhitelist() {
	if in.flags.useList {
		in.writeList(in.flags.whitelist, quoteAll(in.whitelist))
	}
}

func quoteAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "  " + quote(n)
	}
	return out
}

// writeList writes a list file in the game's layout: it starts with an empty
// line, the entries sit two spaces in, and a bare string or an object with a
// reason can be an entry. A trailing newline was not seen in the captures,
// so none is written.
func (in *instance) writeList(path string, entries []string) {
	if path == "" {
		return
	}
	text := "\n[]"
	if len(entries) > 0 {
		text = "\n[\n" + strings.Join(entries, ",\n") + "\n]"
	}
	if err := os.WriteFile(path, []byte(text), world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		in.g.report(err.Error())
	}
}

// quote is a JSON string as the game writes it: no HTML escapes.
func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// foldName is how the 2.x admin list stores a name: accents are dropped
// (ü is stored as u). 1.1 stores the name as typed. Only the accents that
// decompose are known to fold; others were not captured.
func foldName(name string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(name) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
