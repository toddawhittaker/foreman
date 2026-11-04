package gamedir

import (
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// ErrWrongState is returned when the game folder is not in a shape the
// change can start from.
var ErrWrongState = errors.New("the game folder is not in a state Foreman can change")

// ErrNoWorld is returned when the world to present is not a real folder
// under worlds.
var ErrNoWorld = errors.New("that world is not in the game folder")

// preForemanTime is the UTC time format in a pre-foreman folder's name,
// the same shape as snapshot names (ADR 0002).
const preForemanTime = "20060102T150405Z"

var preForemanRE = regexp.MustCompile(`^` + PreForemanPrefix + `[0-9]{8}T[0-9]{6}Z$`)

// afterStep is a test seam called after every change on disk. A test
// returns an error from it to stop the change there, as a crash would.
var afterStep func() error

// Present makes world id the one the game folder presents (SPEC 4.8). The
// game must be stopped. It links current.new to worlds/<id>, makes any
// missing fixed link, then renames current.new over current, so at every
// instant the folder presents one whole world. It refuses an unadopted or
// broken folder.
func Present(dir, id string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	st, err := inspect(root)
	if err != nil {
		return err
	}
	if st.Kind != Fresh && st.Kind != Ready {
		return fmt.Errorf("%w: it is %s", ErrWrongState, st.Kind)
	}
	if err := checkWorld(root, id); err != nil {
		return err
	}
	if err := placeLink(root, CurrentDir+newSuffix, worldTarget(id)); err != nil {
		return err
	}
	return finish(root, "")
}

// CutOver turns a folder with real saves, mods and config into one that
// presents world id, which the caller has already built under worlds
// (SPEC 4.8). The real folders move into pre-foreman-<UTC time>/ beside
// them; nothing is deleted. It returns that folder's name. A cut-over
// stopped part way is finished by Recover.
func CutOver(dir, id string, now time.Time) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	st, err := inspect(root)
	if err != nil {
		return "", err
	}
	if st.Kind != Unadopted {
		return "", fmt.Errorf("%w: it is %s", ErrWrongState, st.Kind)
	}
	if err := checkWorld(root, id); err != nil {
		return "", err
	}
	pre := PreForemanPrefix + now.UTC().Format(preForemanTime)
	if err := world.Mkdir(root, pre); err != nil {
		return "", err
	}
	if err := stepDone(root, "."); err != nil {
		return "", err
	}
	// current.new goes first: it records on disk which world the cut-over
	// is for, so Recover can finish it.
	if err := placeLink(root, CurrentDir+newSuffix, worldTarget(id)); err != nil {
		return "", err
	}
	return pre, finish(root, pre)
}

// Recover runs at startup, with the game stopped. When current is missing
// and current.new names a world, a cut-over or first start stopped part
// way, and Recover finishes it. Otherwise it removes a leftover current.new
// and leftover <name>.new links. It only ever removes links, never a real
// folder.
func Recover(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	cur, err := look(root, CurrentDir)
	if err != nil {
		return err
	}
	next, err := look(root, CurrentDir+newSuffix)
	if err != nil {
		return err
	}
	if cur.kind == missing && next.kind == link {
		if id, ok := idFromTarget(next.target); ok && checkWorld(root, id) == nil {
			pre, err := newestPreForeman(root)
			if err != nil {
				return err
			}
			return finish(root, pre)
		}
	}
	for _, name := range append([]string{CurrentDir}, FixedLinks...) {
		if err := removeLink(root, name+newSuffix); err != nil {
			return err
		}
	}
	return nil
}

// checkWorld refuses an id that is not a real folder under a real worlds
// folder.
func checkWorld(root *os.Root, id string) error {
	if !world.ValidID(id) {
		return fmt.Errorf("%w: %q is not a world id", ErrNoWorld, id)
	}
	w, err := look(root, WorldsDir)
	if err != nil {
		return err
	}
	if w.kind != realDir {
		return fmt.Errorf("%w: worlds is not a real folder", ErrNoWorld)
	}
	e, err := look(root, path.Join(WorldsDir, id))
	if err != nil {
		return err
	}
	if e.kind != realDir {
		return fmt.Errorf("%w: worlds/%s is not a real folder", ErrNoWorld, id)
	}
	return nil
}

// finish puts each fixed link in place, moving a real folder into pre,
// then renames current.new over current.
func finish(root *os.Root, pre string) error {
	for _, name := range FixedLinks {
		if err := placeFixed(root, name, pre); err != nil {
			return err
		}
	}
	if err := root.Rename(CurrentDir+newSuffix, CurrentDir); err != nil {
		return err
	}
	return stepDone(root, ".")
}

// placeFixed makes name a link to current/<name>: a new link beside it,
// the real folder (if any) moved into pre, then the link renamed in.
func placeFixed(root *os.Root, name, pre string) error {
	want := fixedTarget(name)
	e, err := look(root, name)
	if err != nil {
		return err
	}
	switch e.kind {
	case link:
		if e.target != want {
			return fmt.Errorf("%w: %s links to %q, not %s", ErrWrongState, name, e.target, want)
		}
		return removeLink(root, name+newSuffix)
	case other:
		return fmt.Errorf("%w: %s is a file, not a folder or link", ErrWrongState, name)
	}

	if err := placeLink(root, name+newSuffix, want); err != nil {
		return err
	}
	if e.kind == realDir {
		if pre == "" {
			return fmt.Errorf("%w: %s is a real folder and there is no pre-foreman folder to keep it in", ErrWrongState, name)
		}
		kept := path.Join(pre, name)
		if k, err := look(root, kept); err != nil {
			return err
		} else if k.kind != missing {
			return fmt.Errorf("%w: %s already exists, so %s was left where it is", ErrWrongState, kept, name)
		}
		if err := root.Rename(name, kept); err != nil {
			return err
		}
		if err := syncDir(root, pre); err != nil {
			return err
		}
		if err := stepDone(root, "."); err != nil {
			return err
		}
	}
	if err := root.Rename(name+newSuffix, name); err != nil {
		return err
	}
	return stepDone(root, ".")
}

// placeLink makes name a link to target, replacing a leftover link but
// never anything else.
func placeLink(root *os.Root, name, target string) error {
	if err := removeLink(root, name); err != nil {
		return err
	}
	if err := root.Symlink(target, name); err != nil {
		return err
	}
	return stepDone(root, ".")
}

// removeLink removes name if it is a link, does nothing if it is missing,
// and refuses anything else.
func removeLink(root *os.Root, name string) error {
	e, err := look(root, name)
	if err != nil {
		return err
	}
	switch e.kind {
	case missing:
		return nil
	case link:
		if err := root.Remove(name); err != nil {
			return err
		}
		return stepDone(root, ".")
	}
	return fmt.Errorf("%w: %s is not a link, so Foreman left it alone", ErrWrongState, name)
}

// newestPreForeman returns the newest pre-foreman folder's name, or "" when
// there is none.
func newestPreForeman(root *os.Root) (string, error) {
	d, err := root.Open(".")
	if err != nil {
		return "", err
	}
	names, err := d.Readdirnames(-1)
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	slices.Sort(names)
	for _, n := range slices.Backward(names) {
		if !preForemanRE.MatchString(n) {
			continue
		}
		e, err := look(root, n)
		if err != nil {
			return "", err
		}
		if e.kind == realDir {
			return n, nil
		}
	}
	return "", nil
}

// stepDone syncs dir, so the change before it survives a crash, then calls
// the test seam.
func stepDone(root *os.Root, dir string) error {
	if err := syncDir(root, dir); err != nil {
		return err
	}
	if afterStep != nil {
		return afterStep()
	}
	return nil
}

// syncDir is world's folder sync; copied because world keeps it unexported.
func syncDir(root *os.Root, dir string) error {
	d, err := root.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
