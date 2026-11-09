// Package resource reads the resource patches of one planet from the JSON
// a game container writes for a resource count run (SPEC 5.6). The JSON
// comes from the game, so it is checked as untrusted input (SPEC 14.1).
package resource

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/territory"
)

// Limits on what Parse accepts from the game. The measuring run decides
// their final values (SPEC 14.6).
const (
	// MaxJSON is the most bytes Parse reads.
	MaxJSON = 1 << 20
	// MaxPatches is the most patches Parse accepts.
	MaxPatches = 5000
	// maxNumber is the largest whole number the file may hold: the largest
	// that a JSON number carries exactly in the game's floating point.
	maxNumber = 1 << 53
)

// ErrInvalid is JSON from the game that Parse refuses.
var ErrInvalid = errors.New("the game's resource data is not valid")

// namePattern is what a planet, a resource or a patch may be called.
var namePattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

// Resource is one resource's totals over the whole picture.
type Resource struct {
	Total    int64
	Infinite bool
	// Normal is the game's normal amount for an infinite resource, which a
	// well's yield is measured against. It is 0 for an ore.
	Normal int64
	Color  [3]uint8
}

// Patch is one patch of a resource inside the picture. X and Y are the
// label tile, a tile of the patch. Box is the patch's bounding box as
// min x, min y, max x, max y, in tiles and inclusive.
type Patch struct {
	Name     string
	X, Y     int
	Amount   int64
	Entities int
	Box      [4]int
	// Normal is the resource's Normal, copied here so Yield needs only the
	// patch.
	Normal int64
}

// Result is the patches of one planet's picture, largest bounding box first.
type Result struct {
	Planet    string
	Resources map[string]Resource
	Patches   []Patch
}

// Yield is an infinite resource patch's yield as a percentage of the
// resource's normal amount: the sum of its wells' amounts. ok is false for
// an ore.
func Yield(p Patch) (percent float64, ok bool) {
	if p.Normal <= 0 {
		return 0, false
	}
	return 100 * float64(p.Amount) / float64(p.Normal), true
}

// ControlName is the name of the planet's map generator control that sets
// a resource entity: the entity's name without the planet's prefix and with
// hyphens for underscores. An entity with no such control keeps its name.
func ControlName(planet, entity string) string {
	if planet != "" {
		if rest, ok := strings.CutPrefix(entity, planet+"_"); ok && rest != "" {
			entity = rest
		}
	}
	return strings.ReplaceAll(entity, "_", "-")
}

// num is a JSON number, kept as written so a value past 2^53 is refused
// rather than rounded. A string is not a number, though json.Number would
// take "100".
type num string

func (n *num) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || (b[0] != '-' && (b[0] < '0' || b[0] > '9')) {
		return errors.New("not a number")
	}
	*n = num(b)
	return nil
}

// gameJSON is what the count script writes.
type gameJSON struct {
	Planet    string        `json:"planet"`
	Resources resourceTable `json:"resources"`
	Patches   patchTable    `json:"patches"`
}

type resourceJSON struct {
	Total    num   `json:"total"`
	Infinite bool  `json:"infinite"`
	Normal   num   `json:"normal"`
	Color    []num `json:"color"`
}

type patchJSON struct {
	Name   string `json:"name"`
	X      num    `json:"x"`
	Y      num    `json:"y"`
	Amount num    `json:"amount"`
	N      num    `json:"n"`
	Box    []num  `json:"box"`
}

// The game writes an empty table as [] or {}, so each table accepts both
// forms when it is empty.
type resourceTable map[string]resourceJSON

func (t *resourceTable) UnmarshalJSON(b []byte) error {
	var m map[string]resourceJSON
	if err := decodeTable(b, '{', &m); err != nil {
		return err
	}
	*t = m
	return nil
}

type patchTable []patchJSON

func (t *patchTable) UnmarshalJSON(b []byte) error {
	var s []patchJSON
	if err := decodeTable(b, '[', &s); err != nil {
		return err
	}
	*t = s
	return nil
}

// decodeTable decodes b into v, which holds data in the form that opens with
// open. The other bracket is accepted only when empty.
func decodeTable(b []byte, open byte, v any) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || (b[0] != '[' && b[0] != '{') {
		return errors.New("a table is not an object or a list")
	}
	if b[0] != open {
		var inner []json.RawMessage
		if b[0] == '[' {
			if err := json.Unmarshal(b, &inner); err != nil {
				return err
			}
		} else {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(b, &m); err != nil {
				return err
			}
			inner = make([]json.RawMessage, 0, len(m))
			for _, raw := range m {
				inner = append(inner, raw)
			}
		}
		if len(inner) > 0 {
			return errors.New("a table has the wrong form")
		}
		return nil
	}
	return decodeStrict(b, v)
}

// decodeStrict decodes exactly one JSON value into v and refuses unknown
// fields and anything after the value.
func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("data after the JSON")
	}
	return nil
}

// invalid is an ErrInvalid with a reason. The reason holds counts and
// positions, never text from the file.
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Parse checks the game's JSON for a picture of size pixels of the named
// planet and returns its patches, largest bounding box first. It refuses
// JSON over MaxJSON, more than MaxPatches patches, a file for another
// planet, a number that is not a whole number from 0 to 2^53, a patch
// outside the picture or of an unlisted resource, and patch amounts that do
// not add up to their resource's total.
func Parse(b []byte, size int, planet string) (Result, error) {
	if size <= 0 || size > territory.MaxPicture || size%(2*territory.ChunkTiles) != 0 {
		return Result{}, invalid("picture size %d", size)
	}
	if len(b) > MaxJSON {
		return Result{}, invalid("more than %d bytes", MaxJSON)
	}
	var g gameJSON
	if err := decodeStrict(b, &g); err != nil {
		// The decoder's own text can repeat the file's names, so it is
		// left out.
		return Result{}, invalid("not the expected JSON")
	}
	if !namePattern.MatchString(g.Planet) || g.Planet != planet {
		return Result{}, invalid("the file is for another planet")
	}
	if len(g.Patches) > MaxPatches {
		return Result{}, invalid("more than %d patches", MaxPatches)
	}
	out := Result{Planet: g.Planet, Resources: make(map[string]Resource, len(g.Resources))}
	for name, r := range g.Resources {
		res, err := checkResource(name, r)
		if err != nil {
			return Result{}, err
		}
		out.Resources[name] = res
	}
	half := size / 2
	sums := make(map[string]int64, len(out.Resources))
	out.Patches = make([]Patch, 0, len(g.Patches))
	for i, p := range g.Patches {
		patch, err := checkPatch(i, p, half)
		if err != nil {
			return Result{}, err
		}
		res, ok := out.Resources[patch.Name]
		if !ok {
			return Result{}, invalid("patch %d is of a resource not in the table", i)
		}
		// Each amount and the running sum are at most 2^53 before the add,
		// so the sum cannot wrap, and a sum past the total stops here.
		sums[patch.Name] += patch.Amount
		if sums[patch.Name] > res.Total {
			return Result{}, invalid("the patches of a resource add up to more than its total")
		}
		patch.Normal = res.Normal
		out.Patches = append(out.Patches, patch)
	}
	for name, res := range out.Resources {
		if sums[name] != res.Total {
			return Result{}, invalid("the patches of a resource add up to less than its total")
		}
	}
	slices.SortFunc(out.Patches, comparePatches)
	return out, nil
}

func checkResource(name string, r resourceJSON) (Resource, error) {
	if !namePattern.MatchString(name) {
		return Resource{}, invalid("a resource has a bad name")
	}
	total, ok := whole(r.Total, 0, maxNumber)
	normal, ok2 := whole(r.Normal, 0, maxNumber)
	if !ok || !ok2 {
		return Resource{}, invalid("a resource has a total or normal amount that is not a whole number in range")
	}
	if r.Infinite != (normal > 0) {
		return Resource{}, invalid("a resource's normal amount is not above 0 exactly when it is infinite")
	}
	if len(r.Color) != 3 {
		return Resource{}, invalid("a resource's color has %d values", len(r.Color))
	}
	res := Resource{Total: total, Infinite: r.Infinite, Normal: normal}
	for i, c := range r.Color {
		v, ok := whole(c, 0, 255)
		if !ok {
			return Resource{}, invalid("a resource's color is not whole numbers from 0 to 255")
		}
		res.Color[i] = uint8(v) //nolint:gosec // whole() kept it in 0..255
	}
	return res, nil
}

func checkPatch(i int, p patchJSON, half int) (Patch, error) {
	if !namePattern.MatchString(p.Name) {
		return Patch{}, invalid("patch %d has a bad name", i)
	}
	h := int64(half)
	x, okx := whole(p.X, -h, h-1)
	y, oky := whole(p.Y, -h, h-1)
	if !okx || !oky {
		return Patch{}, invalid("patch %d has a label tile outside the picture", i)
	}
	amount, oka := whole(p.Amount, 0, maxNumber)
	n, okn := whole(p.N, 1, maxNumber)
	if !oka || !okn {
		return Patch{}, invalid("patch %d has an amount or count that is not a whole number in range", i)
	}
	if len(p.Box) != 4 {
		return Patch{}, invalid("patch %d has a box of %d values", i, len(p.Box))
	}
	var box [4]int
	for j, c := range p.Box {
		v, ok := whole(c, -h, h-1)
		if !ok {
			return Patch{}, invalid("patch %d has a box outside the picture", i)
		}
		box[j] = int(v)
	}
	if box[0] > box[2] || box[1] > box[3] {
		return Patch{}, invalid("patch %d has a box with its minimum past its maximum", i)
	}
	if int(x) < box[0] || int(x) > box[2] || int(y) < box[1] || int(y) > box[3] {
		return Patch{}, invalid("patch %d has a label tile outside its box", i)
	}
	return Patch{Name: p.Name, X: int(x), Y: int(y), Amount: amount, Entities: int(n), Box: box}, nil
}

// whole reads n as a whole number from lo to hi. A number written as 3e2 or
// 100.0 counts, since the game's JSON writer may use either.
func whole(n num, lo, hi int64) (int64, bool) {
	if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		return i, i >= lo && i <= hi
	}
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil || f < float64(lo) || f > float64(hi) || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// area is the number of tiles a patch's box covers.
func area(p Patch) int {
	return (p.Box[2] - p.Box[0] + 1) * (p.Box[3] - p.Box[1] + 1)
}

func comparePatches(a, b Patch) int {
	return cmp.Or(
		cmp.Compare(area(b), area(a)),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.X, b.X),
		cmp.Compare(a.Y, b.Y),
	)
}
