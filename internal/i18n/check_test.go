package i18n

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The catalogue checks of SPEC 22.5. The checks on templates, scripts and
// stylesheets live in internal/web, next to those files.

// Every language has exactly English's keys, and only the eight languages
// have a folder.
func TestEveryLanguageHasEnglishKeys(t *testing.T) {
	dirs, err := fs.ReadDir(localeFS, "locales")
	if err != nil {
		t.Fatal(err)
	}
	var got, want []string
	for _, d := range dirs {
		got = append(got, d.Name())
	}
	for _, l := range Languages {
		want = append(want, string(l.Tag))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("locale folders %v, want %v", got, want)
	}
	en := catalogue[English]
	for _, l := range Languages {
		for k := range en {
			if _, ok := catalogue[l.Tag][k]; !ok {
				t.Errorf("%s: missing %s", l.Tag, k)
			}
		}
		for k := range catalogue[l.Tag] {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: %s is not in en", l.Tag, k)
			}
		}
	}
}

// Every language has the same area files, and each key starts with its
// file's area.
func TestKeysLiveInTheirArea(t *testing.T) {
	files, err := fs.Glob(localeFS, "locales/*/*.json")
	if err != nil {
		t.Fatal(err)
	}
	areas := map[string]int{}
	for _, f := range files {
		area := strings.TrimSuffix(path.Base(f), ".json")
		areas[area]++
		b, _ := localeFS.ReadFile(f)
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for k := range m {
			if !strings.HasPrefix(k, area+".") {
				t.Errorf("%s: key %s is not in the %s area", f, k, area)
			}
		}
	}
	for a, n := range areas {
		if n != len(Languages) {
			t.Errorf("area %s has %d files, want %d", a, n, len(Languages))
		}
	}
}

// A key may sit in only one file per language; load refuses a second.
func TestNoKeyInTwoFiles(t *testing.T) {
	if _, err := load(); err != nil {
		t.Fatal(err)
	}
}

var (
	rePlaceholder = regexp.MustCompile(`\{([^{}]*)\}`)
	reName        = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	reHTML        = regexp.MustCompile(`<[a-zA-Z/!]|&[a-zA-Z#][a-zA-Z0-9]*;`)
)

func placeholders(e entry) []string {
	set := map[string]bool{}
	texts := []string{e.text}
	for _, s := range e.plural {
		texts = append(texts, s)
	}
	for _, s := range texts {
		for _, m := range rePlaceholder.FindAllStringSubmatch(s, -1) {
			set[m[1]] = true
		}
	}
	var out []string
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Placeholders are well formed and the same in every language; a plural
// entry is one in every language; no value holds HTML.
func TestPlaceholdersMatch(t *testing.T) {
	for k, en := range catalogue[English] {
		want := placeholders(en)
		for _, l := range Languages {
			e := catalogue[l.Tag][k]
			got := placeholders(e)
			for _, n := range got {
				if !reName.MatchString(n) {
					t.Errorf("%s %s: bad placeholder {%s}", l.Tag, k, n)
				}
			}
			// A plural form may leave out {count} where the form says the
			// number in words, as English "one" does.
			if e.plural != nil {
				got = slices.DeleteFunc(slices.Clone(got), func(s string) bool { return s == "count" })
				want := slices.DeleteFunc(slices.Clone(want), func(s string) bool { return s == "count" })
				if !slices.Equal(got, want) {
					t.Errorf("%s %s: placeholders %v, want %v", l.Tag, k, got, want)
				}
			} else if !slices.Equal(got, want) {
				t.Errorf("%s %s: placeholders %v, want %v", l.Tag, k, got, want)
			}
			if (e.plural == nil) != (en.plural == nil) {
				t.Errorf("%s %s: plural in one language and not the other", l.Tag, k)
			}
			for _, s := range append([]string{e.text}, mapValues(e.plural)...) {
				if reHTML.MatchString(s) {
					t.Errorf("%s %s: HTML in %q", l.Tag, k, s)
				}
			}
		}
	}
}

func mapValues(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// Each plural entry has exactly the categories its language uses for whole
// numbers, as CLDR gives them (SPEC 22.4).
func TestPluralCategories(t *testing.T) {
	for _, l := range Languages {
		used := map[string]bool{}
		for n := 0; n <= 1000; n++ {
			used[Category(l.Tag, n)] = true
		}
		for k, e := range catalogue[l.Tag] {
			if e.plural == nil {
				continue
			}
			for c := range used {
				if _, ok := e.plural[c]; !ok {
					t.Errorf("%s %s: no %q form", l.Tag, k, c)
				}
			}
			for c := range e.plural {
				if !used[c] {
					t.Errorf("%s %s: %q is not a form %s uses", l.Tag, k, c, l.Tag)
				}
			}
		}
	}
}

// translated.json matches the English: a key whose English changed must
// be translated again, then make i18n-hash run.
func TestTranslationsAreCurrent(t *testing.T) {
	b, err := os.ReadFile("translated.json")
	if err != nil {
		t.Fatal(err)
	}
	var recorded map[string]string
	if err := json.Unmarshal(b, &recorded); err != nil {
		t.Fatal(err)
	}
	for k, h := range Hashes() {
		r, ok := recorded[k]
		switch {
		case !ok:
			t.Errorf("%s is not in translated.json: translate it into every language, then run make i18n-hash", k)
		case r != h:
			t.Errorf("the English of %s changed since it was translated: update every language, then run make i18n-hash", k)
		}
	}
	for k := range recorded {
		if _, ok := catalogue[English][k]; !ok {
			t.Errorf("translated.json lists %s, which is gone: run make i18n-hash", k)
		}
	}
}
