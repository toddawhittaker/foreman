package factorymap

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

const goodSurface = `{"index":1,"name":"nauvis","kind":"planet","chunks":4,"min_x":-1,"min_y":-1,"max_x":0,"max_y":0,` +
	`"chart":"surface-1.jsonl","entities":"surface-1-entities.jsonl","entities_total":10,"entities_truncated":false}`

func manifestWith(surfaces ...string) string {
	return `{"format":1,"game_version":"2.0.77","tick":8352001,"surfaces":[` + strings.Join(surfaces, ",") + `]}`
}

func TestParseManifestAcceptsTheHelpersOutput(t *testing.T) {
	m, err := ParseManifest(strings.NewReader(manifestWith(goodSurface)))
	if err != nil {
		t.Fatal(err)
	}
	s := m.Surfaces[0]
	if m.GameVersion != "2.0.77" || m.Tick != 8352001 || s.Index != 1 || s.MinX != -1 || s.EntitiesTotal != 10 || s.Chart != ChartFile(1) {
		t.Errorf("parsed %+v", m)
	}
	if _, err := ParseManifest(strings.NewReader(manifestWith())); err != nil {
		t.Errorf("a world with no surfaces: %v", err)
	}
	// An empty surface (no charted chunk) is fine.
	empty := strings.Replace(goodSurface, `"chunks":4`, `"chunks":0`, 1)
	if _, err := ParseManifest(strings.NewReader(manifestWith(empty))); err != nil {
		t.Errorf("empty surface: %v", err)
	}
}

func TestParseManifestRefuses(t *testing.T) {
	sub := func(old, new string) string { return strings.Replace(goodSurface, old, new, 1) }
	surfaceAt := func(i int) string {
		return fmt.Sprintf(`{"index":%d,"name":"s","kind":"surface","chunks":1,"min_x":0,"min_y":0,"max_x":0,"max_y":0,`+
			`"chart":"surface-%d.jsonl","entities":"surface-%d-entities.jsonl","entities_total":0,"entities_truncated":false}`, i, i, i)
	}
	many := make([]string, MaxSurfaces+1)
	for i := range many {
		many[i] = surfaceAt(i)
	}
	cases := map[string]string{
		"empty":                    ``,
		"not json":                 `nope`,
		"two documents":            manifestWith(goodSurface) + `{}`,
		"trailing text":            manifestWith(goodSurface) + ` x`,
		"unknown top field":        strings.Replace(manifestWith(goodSurface), `"tick"`, `"extra":1,"tick"`, 1),
		"unknown surface field":    manifestWith(sub(`"kind"`, `"extra":1,"kind"`)),
		"format 2":                 strings.Replace(manifestWith(goodSurface), `"format":1`, `"format":2`, 1),
		"format 0":                 strings.Replace(manifestWith(goodSurface), `"format":1`, `"format":0`, 1),
		"no version":               strings.Replace(manifestWith(goodSurface), `"2.0.77"`, `""`, 1),
		"control in version":       strings.Replace(manifestWith(goodSurface), `"2.0.77"`, `"2.0\n77"`, 1),
		"negative tick":            strings.Replace(manifestWith(goodSurface), `8352001`, `-1`, 1),
		"tick past 2^53":           strings.Replace(manifestWith(goodSurface), `8352001`, `9007199254740993`, 1),
		"fractional tick":          strings.Replace(manifestWith(goodSurface), `8352001`, `1.5`, 1),
		"exponent tick":            strings.Replace(manifestWith(goodSurface), `8352001`, `1e3`, 1),
		"too many surfaces":        manifestWith(many...),
		"duplicate index":          manifestWith(goodSurface, goodSurface),
		"negative index":           manifestWith(sub(`"index":1`, `"index":-1`)),
		"empty name":               manifestWith(sub(`"nauvis"`, `""`)),
		"long name":                manifestWith(sub(`"nauvis"`, `"`+strings.Repeat("n", 201)+`"`)),
		"control in name":          manifestWith(sub(`"nauvis"`, `"na\u0007uvis"`)),
		"unknown kind":             manifestWith(sub(`"planet"`, `"galaxy"`)),
		"negative chunks":          manifestWith(sub(`"chunks":4`, `"chunks":-1`)),
		"too many chunks":          manifestWith(sub(`"chunks":4`, `"chunks":65537`)),
		"chunks beyond the bounds": manifestWith(sub(`"chunks":4`, `"chunks":5`)),
		"inverted bounds":          manifestWith(sub(`"max_x":0`, `"max_x":-5`)),
		"bounds far out":           manifestWith(sub(`"max_y":0`, `"max_y":99999999`)),
		"negative total":           manifestWith(sub(`"entities_total":10`, `"entities_total":-1`)),
		"too many buildings":       manifestWith(sub(`"entities_total":10`, `"entities_total":2000001`)),
		"chart in another folder":  manifestWith(sub(`"surface-1.jsonl"`, `"../surface-1.jsonl"`)),
		"chart absolute":           manifestWith(sub(`"surface-1.jsonl"`, `"/etc/passwd"`)),
		"chart for another index":  manifestWith(sub(`"surface-1.jsonl"`, `"surface-2.jsonl"`)),
		"entities with a slash":    manifestWith(sub(`"surface-1-entities.jsonl"`, `"a/surface-1-entities.jsonl"`)),
		"no entities name":         manifestWith(sub(`"surface-1-entities.jsonl"`, `""`)),
		"huge":                     manifestWith(goodSurface) + strings.Repeat(" ", MaxManifestBytes),
	}
	for name, body := range cases {
		_, err := ParseManifest(strings.NewReader(body))
		if !errors.Is(err, ErrManifest) {
			t.Errorf("%s: err = %v, want ErrManifest", name, err)
		}
	}
}

// The JSON decoder turns bad UTF-8 into U+FFFD, so a name is always valid text.
func TestParseManifestNamesAreValidText(t *testing.T) {
	body := manifestWith(strings.Replace(goodSurface, `"nauvis"`, "\"na\xffuvis\"", 1))
	m, err := ParseManifest(strings.NewReader(body))
	if err != nil || !utf8.ValidString(m.Surfaces[0].Name) {
		t.Errorf("name %q, err %v", m.Surfaces[0].Name, err)
	}
}

func TestParseManifestBuildingsInAll(t *testing.T) {
	a := strings.Replace(goodSurface, `"entities_total":10`, `"entities_total":1500000`, 1)
	b := strings.NewReplacer(`"index":1`, `"index":2`, `surface-1`, `surface-2`, `"entities_total":10`, `"entities_total":500001`).Replace(goodSurface)
	if _, err := ParseManifest(strings.NewReader(manifestWith(a, b))); !errors.Is(err, ErrManifest) {
		t.Errorf("two surfaces over the total: %v", err)
	}
	b = strings.Replace(b, `500001`, `500000`, 1)
	if _, err := ParseManifest(strings.NewReader(manifestWith(a, b))); err != nil {
		t.Errorf("exactly at the total: %v", err)
	}
}

func TestParseManifestStopsReadingAtTheLimit(t *testing.T) {
	r := &countingReader{}
	if _, err := ParseManifest(r); !errors.Is(err, ErrManifest) {
		t.Errorf("endless input: %v", err)
	}
	if r.n > MaxManifestBytes+4096 {
		t.Errorf("read %d bytes of an endless input", r.n)
	}
}

type countingReader struct{ n int }

func (c *countingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	c.n += len(p)
	return len(p), nil
}
