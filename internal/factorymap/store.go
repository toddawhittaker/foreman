package factorymap

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Store is a world's kept pictures, in factorymaps/<world-id>/ under the
// data directory: never in a world or a snapshot, one drawing per world (SPEC
// 5.16).
//
// Layout of factorymaps/<id>/: factorymap.json says which generation folder
// holds the current drawing and gives its facts; each generation folder g-*
// holds the PNGs. Replace writes a whole new generation, then swaps
// factorymap.json in one rename, then deletes the old generation, so a reader
// sees the old drawing or the new one, never a mix.
type Store struct {
	dataDir string
	id      string
}

// ErrInvalid wraps a refusal of a world id, a picture name, a surface or a
// layer the store will not touch.
var ErrInvalid = errors.New("factory map store: invalid")

const (
	storeDir     = "factorymaps"
	factsFile    = "factorymap.json"
	factsFormat  = 1
	maxFactsSize = 1 << 20
	genPrefix    = "g-"
)

// mu serializes every change to any store. A drawing is rare and a few
// seconds of waiting is fine, and one lock cannot deadlock.
var mu sync.Mutex

// retries is how often a reader starts over when a Replace removes the
// generation it was about to open.
const retries = 3

// facts is factorymap.json.
type facts struct {
	Format      int            `json:"format"`
	Dir         string         `json:"dir"`
	Save        string         `json:"save"`
	Size        int64          `json:"size"`
	Modified    time.Time      `json:"modified"`
	DrawnAt     time.Time      `json:"drawn_at"`
	GameVersion string         `json:"game_version"`
	Surfaces    []factsSurface `json:"surfaces"`
	Failure     string         `json:"failure,omitempty"`
}

type factsSurface struct {
	Index         int    `json:"index"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	TilesPerPixel int    `json:"tiles_per_pixel"`
}

func (f facts) drawing() Drawing {
	d := Drawing{
		Basis:       saveplayers.Basis{Save: f.Save, Size: f.Size, Modified: f.Modified},
		DrawnAt:     f.DrawnAt,
		GameVersion: f.GameVersion,
		Failure:     f.Failure,
	}
	for _, s := range f.Surfaces {
		d.Surfaces = append(d.Surfaces, Surface(s))
	}
	return d
}

func factsOf(d Drawing, dir string) facts {
	f := facts{
		Format:      factsFormat,
		Dir:         dir,
		Save:        d.Basis.Save,
		Size:        d.Basis.Size,
		Modified:    d.Basis.Modified,
		DrawnAt:     d.DrawnAt,
		GameVersion: d.GameVersion,
		Failure:     d.Failure,
	}
	for _, s := range d.Surfaces {
		f.Surfaces = append(f.Surfaces, factsSurface(s))
	}
	return f
}

var (
	genPattern = regexp.MustCompile(`^g-[0-9a-f]{16}$`)
	pngPattern = regexp.MustCompile(`^surface-(0|[1-9][0-9]{0,15})(-[a-z]+)?\.png$`)
)

// PNGName is the file name of a surface's picture: the base picture when
// layer is "", else the layer's. It is the key Replace takes.
func PNGName(surface int, layer Category) string {
	if layer == "" {
		return "surface-" + strconv.Itoa(surface) + ".png"
	}
	return "surface-" + strconv.Itoa(surface) + "-" + string(layer) + ".png"
}

// Open opens the world's store, which may be empty. It refuses an id that is
// not a world id, so no path can leave the store.
func Open(dataDir, id string) (*Store, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("%w: no data directory", ErrInvalid)
	}
	if !world.ValidID(id) {
		return nil, fmt.Errorf("%w: world id %q", ErrInvalid, id)
	}
	return &Store{dataDir: dataDir, id: id}, nil
}

// worldRoot opens factorymaps/<id>, refusing a link in place of either
// folder. With create it makes missing folders; otherwise a missing folder
// is fs.ErrNotExist. The caller closes the root.
func (s *Store) worldRoot(create bool) (*os.Root, error) {
	data, err := os.OpenRoot(s.dataDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = data.Close() }()
	cur := data
	for _, name := range []string{storeDir, s.id} {
		next, err := openFolder(cur, name, create)
		if cur != data {
			_ = cur.Close()
		}
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}

func openFolder(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) && create {
		if err = world.Mkdir(parent, name); err == nil {
			info, err = parent.Lstat(name)
		}
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", name)
	}
	return parent.OpenRoot(name)
}

// readFacts reads and checks factorymap.json from the world's folder.
func readFacts(root *os.Root) (facts, error) {
	f, err := openRegular(root, factsFile)
	if err != nil {
		return facts{}, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxFactsSize+1))
	if err != nil {
		return facts{}, err
	}
	if len(data) > maxFactsSize {
		return facts{}, fmt.Errorf("%s is too large", factsFile)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var fc facts
	if err := dec.Decode(&fc); err != nil {
		return facts{}, fmt.Errorf("%s: %w", factsFile, err)
	}
	if fc.Format != factsFormat || !genPattern.MatchString(fc.Dir) || len(fc.Surfaces) > MaxSurfaces {
		return facts{}, fmt.Errorf("%s: not a drawing this Foreman wrote", factsFile)
	}
	for _, s := range fc.Surfaces {
		if s.Index < 0 {
			return facts{}, fmt.Errorf("%s: bad surface index", factsFile)
		}
	}
	return fc, nil
}

// Replace keeps d and its PNGs, by file name (PNGName), in place of the
// previous drawing, so a reader sees the old drawing or the new one, never a
// mix. Every surface of d needs its base picture, and a layer picture may only
// belong to a surface of d and a known category.
//
// A d with Failure set and no PNGs records the failure beside the previous
// drawing, which stays whole (its basis, time and pictures are kept and d's
// other fields are ignored, though they must still be well formed). With no
// previous drawing, the failure is kept alone, with d's basis and time and no
// surfaces. A d with no Failure replaces everything and so clears a failure.
// The failure text is cut to MaxFailureBytes.
//
// Replace first removes what a crash left behind: temporary files in the
// world's folder and, at the end, generation folders no record points at.
func (s *Store) Replace(d Drawing, pngs map[string][]byte) error {
	d.Failure = cutText(d.Failure, MaxFailureBytes)
	failureOnly := d.Failure != "" && len(pngs) == 0
	var err error
	if failureOnly {
		err = checkShape(d)
	} else {
		err = checkDrawing(d, pngs)
	}
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	root, err := s.worldRoot(true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	clearPartials(root)

	old, err := readFacts(root)
	if err != nil {
		// A missing or damaged record is replaced.
		old = facts{}
	}
	if !failureOnly {
		return s.commit(root, factsOf(d, newGen()), pngs, old.Dir)
	}
	if old.Dir == "" {
		d.Surfaces = nil
		return s.commit(root, factsOf(d, newGen()), nil, "")
	}
	old.Failure = d.Failure
	b, err := marshalFacts(old)
	if err != nil {
		return err
	}
	if err := world.WriteFileAtomic(root, factsFile, b); err != nil {
		return err
	}
	pruneGenerations(root, old.Dir)
	return nil
}

// MaxFailureBytes is the most of a failure text the store keeps, so a failure
// can never make the record too large to read.
const MaxFailureBytes = 4 << 10

// cutText cuts s to at most max bytes without splitting a character.
func cutText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// clearPartials removes the temporary files an interrupted write left in the
// world's folder (world.PrefixPartial), such as a half-written
// factorymap.json. It is best effort, and the caller holds mu, so no write of
// this process is in flight.
func clearPartials(root *os.Root) {
	for _, name := range entryNames(root) {
		if strings.HasPrefix(name, world.PrefixPartial) {
			_ = root.RemoveAll(name)
		}
	}
}

// entryNames lists the entries of the folder root holds.
func entryNames(root *os.Root) []string {
	dir, err := root.Open(".")
	if err != nil {
		return nil
	}
	defer func() { _ = dir.Close() }()
	names, _ := dir.Readdirnames(-1)
	return names
}

// commit writes a generation folder holding pngs, points factorymap.json at
// it, then deletes every other generation. A failure removes the new folder
// and leaves the old drawing in place.
func (s *Store) commit(root *os.Root, f facts, pngs map[string][]byte, oldDir string) error {
	if err := world.Mkdir(root, f.Dir); err != nil {
		return err
	}
	err := func() error {
		sub, err := root.OpenRoot(f.Dir)
		if err != nil {
			return err
		}
		defer func() { _ = sub.Close() }()
		for name, b := range pngs {
			if err := world.WriteFileAtomic(sub, name, b); err != nil {
				return err
			}
		}
		b, err := marshalFacts(f)
		if err != nil {
			return err
		}
		return world.WriteFileAtomic(root, factsFile, b)
	}()
	if err != nil {
		_ = root.RemoveAll(f.Dir)
		return err
	}
	pruneGenerations(root, f.Dir)
	return nil
}

// pruneGenerations removes every folder that is not the current generation.
// It is best effort: a leftover is harmless and the next Replace removes it.
func pruneGenerations(root *os.Root, keep string) {
	for _, name := range entryNames(root) {
		if name != keep && genPattern.MatchString(name) {
			_ = root.RemoveAll(name)
		}
	}
}

func newGen() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // the system's random source is gone; nothing safe remains
	}
	return genPrefix + hex.EncodeToString(b[:])
}

// marshalFacts encodes the record. It fails, rather than writing a record
// that cannot be read back, on a value json refuses (a year past 9999).
func marshalFacts(f facts) ([]byte, error) {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if len(b) > maxFactsSize {
		return nil, fmt.Errorf("%w: the drawing's facts are too large", ErrInvalid)
	}
	return append(b, '\n'), nil
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// maxSavePathBytes is the longest save name a drawing's basis may hold.
const maxSavePathBytes = 255

// checkShape refuses a drawing whose facts the store could not write or read
// back: too many or malformed surfaces, over-long text, impossible numbers or
// times. It does not look at pictures.
func checkShape(d Drawing) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
	}
	if len(d.Surfaces) > MaxSurfaces {
		return bad("%d surfaces", len(d.Surfaces))
	}
	if len(d.Basis.Save) > maxSavePathBytes || len(d.GameVersion) > maxVersionLen || len(d.Failure) > MaxFailureBytes {
		return bad("text too long")
	}
	if d.Basis.Size < 0 || d.Basis.Size > maxWholeNumber {
		return bad("basis size")
	}
	for _, t := range []time.Time{d.Basis.Modified, d.DrawnAt} {
		if y := t.Year(); y < 0 || y > 9999 {
			return bad("time out of range")
		}
	}
	indexes := make(map[int]bool, len(d.Surfaces))
	for _, sf := range d.Surfaces {
		if sf.Index < 0 || sf.Index > maxWholeNumber || indexes[sf.Index] {
			return bad("surface %d", sf.Index)
		}
		indexes[sf.Index] = true
		switch sf.Kind {
		case "planet", "platform", "surface":
		default:
			return bad("surface %d has kind %q", sf.Index, sf.Kind)
		}
		if len(sf.Name) > maxNameBytes || sf.Width < 0 || sf.Height < 0 || sf.Width > MaxSidePixels || sf.Height > MaxSidePixels ||
			sf.TilesPerPixel < 0 || sf.TilesPerPixel > 1<<24 {
			return bad("surface %d has a bad name or size", sf.Index)
		}
	}
	return nil
}

// checkDrawing refuses a drawing and pictures the store could not serve.
func checkDrawing(d Drawing, pngs map[string][]byte) error {
	if err := checkShape(d); err != nil {
		return err
	}
	indexes := make(map[int]bool, len(d.Surfaces))
	for _, sf := range d.Surfaces {
		indexes[sf.Index] = true
		if _, ok := pngs[PNGName(sf.Index, "")]; !ok {
			return fmt.Errorf("%w: no base picture for surface %d", ErrInvalid, sf.Index)
		}
	}
	for name, b := range pngs {
		m := pngPattern.FindStringSubmatch(name)
		if m == nil {
			return fmt.Errorf("%w: picture name %q", ErrInvalid, name)
		}
		i, err := strconv.Atoi(m[1])
		if err != nil || !indexes[i] {
			return fmt.Errorf("%w: picture %q is for no surface of the drawing", ErrInvalid, name)
		}
		if m[2] != "" && !slices.Contains(Categories, Category(m[2][1:])) {
			return fmt.Errorf("%w: picture %q is for no category", ErrInvalid, name)
		}
		if !bytes.HasPrefix(b, pngSignature) {
			return fmt.Errorf("%w: picture %q is not a PNG", ErrInvalid, name)
		}
	}
	return nil
}

// Read returns the kept drawing's facts; ok is false when there is none.
func (s *Store) Read() (d Drawing, ok bool, err error) {
	root, err := s.worldRoot(false)
	if errors.Is(err, fs.ErrNotExist) {
		return Drawing{}, false, nil
	}
	if err != nil {
		return Drawing{}, false, err
	}
	defer func() { _ = root.Close() }()
	f, err := readFacts(root)
	if errors.Is(err, fs.ErrNotExist) {
		return Drawing{}, false, nil
	}
	if err != nil {
		return Drawing{}, false, err
	}
	return f.drawing(), true, nil
}

// checkPicture refuses a layer or surface that no drawing has.
func (f facts) checkPicture(surface int, layer Category) error {
	if layer != "" && !slices.Contains(Categories, layer) {
		return fmt.Errorf("%w: layer %q", ErrInvalid, layer)
	}
	for _, sf := range f.Surfaces {
		if sf.Index == surface {
			return nil
		}
	}
	return fmt.Errorf("surface %d: %w", surface, fs.ErrNotExist)
}

// OpenPNG opens one picture of a surface: the base picture when layer is "",
// else the layer's. A missing drawing, surface or picture is fs.ErrNotExist;
// a layer that is no category is ErrInvalid. The caller closes it.
func (s *Store) OpenPNG(surface int, layer Category) (*os.File, error) {
	for range retries {
		f, err := s.openPNG(surface, layer)
		if !errors.Is(err, errGone) {
			return f, err
		}
	}
	return nil, fmt.Errorf("the drawing changed while it was read: %w", fs.ErrNotExist)
}

// errGone says a generation was removed between reading factorymap.json and
// opening a picture, so the reader starts over on the new drawing.
var errGone = errors.New("generation removed")

func (s *Store) openPNG(surface int, layer Category) (*os.File, error) {
	root, err := s.worldRoot(false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	fc, err := readFacts(root)
	if err != nil {
		return nil, err
	}
	if err := fc.checkPicture(surface, layer); err != nil {
		return nil, err
	}
	f, err := openRegular(root, path.Join(fc.Dir, PNGName(surface, layer)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// A missing picture is real when the generation is still the
			// current one, and a race when Replace has swapped it.
			if again, e := readFacts(root); e == nil && again.Dir != fc.Dir {
				return nil, errGone
			}
		}
		return nil, err
	}
	return f, nil
}

// openRegular opens a stored file without blocking and refuses a link or
// anything but a regular file, so a named pipe planted in the folder cannot
// hang a reader.
func openRegular(root *os.Root, name string) (*os.File, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%s is not a file: %w", name, fs.ErrInvalid)
	}
	return f, nil
}

// Composite returns the base picture of a surface with the layers stacked,
// in the order given, for the combined download. A layer the drawing has no
// picture for has nothing to draw and is skipped; a layer that is no category
// is ErrInvalid. Pictures are decoded under MaxSidePixels, and a layer of
// another size than the base is refused.
func (s *Store) Composite(surface int, layers []Category) (image.Image, error) {
	for _, l := range layers {
		if !slices.Contains(Categories, l) {
			return nil, fmt.Errorf("%w: layer %q", ErrInvalid, l)
		}
	}
	for range retries {
		img, err := s.composite(surface, layers)
		if !errors.Is(err, errGone) {
			return img, err
		}
	}
	return nil, fmt.Errorf("the drawing changed while it was read: %w", fs.ErrNotExist)
}

func (s *Store) composite(surface int, layers []Category) (image.Image, error) {
	root, err := s.worldRoot(false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	fc, err := readFacts(root)
	if err != nil {
		return nil, err
	}
	if err := fc.checkPicture(surface, ""); err != nil {
		return nil, err
	}
	base, err := decodePicture(root, fc, surface, "")
	if err != nil {
		return nil, err
	}
	out := image.NewNRGBA(base.Bounds())
	draw.Draw(out, out.Bounds(), base, base.Bounds().Min, draw.Src)
	done := map[Category]bool{}
	for _, l := range layers {
		if done[l] {
			continue
		}
		done[l] = true
		img, err := decodePicture(root, fc, surface, l)
		if errors.Is(err, fs.ErrNotExist) {
			if again, e := readFacts(root); e == nil && again.Dir != fc.Dir {
				return nil, errGone
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if img.Bounds() != base.Bounds() {
			return nil, fmt.Errorf("layer %s of surface %d is not the size of its picture", l, surface)
		}
		draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Over)
	}
	return out, nil
}

// decodePicture decodes one stored PNG, checking its size first so a large
// file cannot make a large image.
func decodePicture(root *os.Root, fc facts, surface int, layer Category) (image.Image, error) {
	name := PNGName(surface, layer)
	f, err := openRegular(root, path.Join(fc.Dir, name))
	if err != nil {
		if layer == "" && errors.Is(err, fs.ErrNotExist) {
			if again, e := readFacts(root); e == nil && again.Dir != fc.Dir {
				return nil, errGone
			}
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxSidePixels || cfg.Height > MaxSidePixels {
		return nil, fmt.Errorf("%s: %d by %d pixels is outside the limit", name, cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return img, nil
}

// Remove deletes a world's kept pictures; none is not an error. It refuses an
// id that is not a world id.
func Remove(dataDir, id string) error {
	s, err := Open(dataDir, id)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	data, err := os.OpenRoot(dataDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = data.Close() }()
	info, err := data.Lstat(storeDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder", storeDir)
	}
	top, err := data.OpenRoot(storeDir)
	if err != nil {
		return err
	}
	defer func() { _ = top.Close() }()
	// A link in the world's place is removed as a link, never followed.
	if err := top.RemoveAll(s.id); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Sweep removes every store folder under the data directory whose world id
// keep rejects, so the pictures of a world that no longer exists go (SPEC
// 5.16). Foreman calls it at startup. Entries whose names are not world ids
// are not stores and stay. keep is called without the store lock, so it may
// use the store. A link in a store's place is removed as a link, never
// followed. Sweep goes on after a failure and returns all of them.
func Sweep(dataDir string, keep func(id string) bool) error {
	data, err := os.OpenRoot(dataDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = data.Close() }()
	info, err := data.Lstat(storeDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder", storeDir)
	}
	top, err := data.OpenRoot(storeDir)
	if err != nil {
		return err
	}
	defer func() { _ = top.Close() }()
	var gone []string
	for _, name := range entryNames(top) {
		if world.ValidID(name) && !keep(name) {
			gone = append(gone, name)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	var errs []error
	for _, name := range gone {
		if err := top.RemoveAll(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
