// Package gamedir owns the layout of the adopted game folder (SPEC 4.8):
// worlds/<id>/, a relative link current to worlds/<id>, and fixed relative
// links saves, mods and config to current/saves and so on. It reads links
// with Readlink and checks their exact text; it never follows one, and every
// call goes through os.Root so nothing can reach outside the folder
// (SPEC 14.5).
package gamedir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/toddawhittaker/foreman/internal/world"
)

// Names in the game folder (SPEC 4.8).
const (
	WorldsDir  = "worlds"
	CurrentDir = "current"
	// newSuffix marks a link made beside its final name and renamed into
	// place, so the name always holds either the old entry or the new one.
	newSuffix = ".new"
	// PreForemanPrefix starts the folder that keeps the original saves,
	// mods and config at first adoption, followed by the UTC time.
	PreForemanPrefix = "pre-foreman-"
)

// FixedLinks are the names the game reads, each a link into current.
var FixedLinks = []string{world.SavesDir, world.ModsDir, world.ConfigDir}

// Kind is the overall shape of the game folder.
type Kind int

// The four shapes a game folder can be in.
const (
	// Fresh: no current and no real saves, mods or config. Foreman makes
	// the links at first start.
	Fresh Kind = iota + 1
	// Unadopted: real saves, mods or config folders and no current; the
	// adoption form is shown.
	Unadopted
	// Ready: current links to a valid world and the fixed links are right.
	Ready
	// Broken: anything else. Server control is refused.
	Broken
)

func (k Kind) String() string {
	switch k {
	case Fresh:
		return "fresh"
	case Unadopted:
		return "unadopted"
	case Ready:
		return "ready"
	case Broken:
		return "broken"
	}
	return "unknown"
}

// State is what Inspect found.
type State struct {
	Kind Kind
	// ActiveID is the world current links to, when Kind is Ready.
	ActiveID string
	// Path (relative to the game folder) and Reason say what is wrong,
	// when Kind is Broken.
	Path   string
	Reason string
}

// Inspect reports the shape of the game folder dir.
func Inspect(dir string) (State, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = root.Close() }()
	return inspect(root)
}

type entryKind int

const (
	missing entryKind = iota
	realDir
	link
	other
)

type entry struct {
	kind   entryKind
	target string // the link's text, when kind is link
}

// look reports what name is without following it.
func look(root *os.Root, name string) (entry, error) {
	fi, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return entry{kind: missing}, nil
	}
	if err != nil {
		return entry{}, err
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		target, err := root.Readlink(name)
		if err != nil {
			return entry{}, err
		}
		return entry{kind: link, target: target}, nil
	case fi.IsDir():
		return entry{kind: realDir}, nil
	}
	return entry{kind: other}, nil
}

func fixedTarget(name string) string { return CurrentDir + "/" + name }

func worldTarget(id string) string { return WorldsDir + "/" + id }

// idFromTarget returns the world id a current link names, or false when
// its text is anything but exactly worlds/<valid id>.
func idFromTarget(target string) (string, bool) {
	id, ok := strings.CutPrefix(target, WorldsDir+"/")
	if !ok || !world.ValidID(id) {
		return "", false
	}
	return id, true
}

func broken(p, format string, args ...any) State {
	return State{Kind: Broken, Path: p, Reason: fmt.Sprintf(format, args...)}
}

func inspect(root *os.Root) (State, error) {
	w, err := look(root, WorldsDir)
	if err != nil {
		return State{}, err
	}
	switch w.kind {
	case link:
		return broken(WorldsDir, "worlds is a link. Foreman only uses a real worlds folder"), nil
	case other:
		return broken(WorldsDir, "worlds is a file, not a folder"), nil
	}

	fixed := make(map[string]entry, len(FixedLinks))
	for _, name := range FixedLinks {
		e, err := look(root, name)
		if err != nil {
			return State{}, err
		}
		if e.kind == other {
			return broken(name, "%s is a file, not a folder or link", name), nil
		}
		if e.kind == link && e.target != fixedTarget(name) {
			return broken(name, "%s links to %q, not %s", name, e.target, fixedTarget(name)), nil
		}
		fixed[name] = e
	}

	cur, err := look(root, CurrentDir)
	if err != nil {
		return State{}, err
	}
	switch cur.kind {
	case missing:
		return withoutCurrent(fixed), nil
	case link:
	default:
		return broken(CurrentDir, "current is not a link"), nil
	}

	id, ok := idFromTarget(cur.target)
	if !ok {
		return broken(CurrentDir, "current links to %q, not worlds/<world id>", cur.target), nil
	}
	if w.kind == missing {
		return broken(WorldsDir, "current names world %s, but there is no worlds folder", id), nil
	}
	we, err := look(root, path.Join(WorldsDir, id))
	if err != nil {
		return State{}, err
	}
	if we.kind != realDir {
		return broken(worldTarget(id), "current names world %s, but worlds/%s is not a folder", id, id), nil
	}
	for _, name := range FixedLinks {
		switch fixed[name].kind {
		case missing:
			return broken(name, "%s is missing; it should link to %s", name, fixedTarget(name)), nil
		case realDir:
			return broken(name, "%s is a real folder; it should link to %s", name, fixedTarget(name)), nil
		}
	}
	return State{Kind: Ready, ActiveID: id}, nil
}

// withoutCurrent sorts a folder with no current link. Fixed links already
// in place mean a first start stopped part way, which is still Fresh.
func withoutCurrent(fixed map[string]entry) State {
	links, dirs := 0, 0
	for _, name := range FixedLinks {
		switch fixed[name].kind {
		case link:
			links++
		case realDir:
			dirs++
		}
	}
	switch {
	case dirs == 0:
		return State{Kind: Fresh}
	case links == 0:
		return State{Kind: Unadopted}
	}
	for _, name := range FixedLinks {
		if fixed[name].kind == link {
			return broken(name, "%s is a link but other game folders are still real folders, and current is missing. An adoption may have stopped part way", name)
		}
	}
	return State{Kind: Broken} // not reached: links > 0
}
