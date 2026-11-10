package web

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// A resource label drawn into a downloaded picture reads as the page writes
// it with the browser's Intl.NumberFormat: an amount in compact notation
// with at most one fraction digit ("12.3M", "1,2 Mio.", "1234.6万"), and a
// yield as a whole percentage. Go has no compact notation, so the CLDR
// patterns of the eight languages are kept here (SPEC 5.6, 22.4).

// compactUnit is one magnitude's CLDR short pattern: the number of integer
// digits it shows and the unit after them, the separator included.
type compactUnit struct {
	zeros int
	unit  string
}

// numberStyle is how one language writes the labels' numbers.
type numberStyle struct {
	decimal, group string
	// percentGrouping is the fewest integer digits a percentage is grouped
	// at: 4, or 5 where CLDR's minimum grouping is 2. A compact amount is
	// always grouped from 5 digits.
	percentGrouping int
	percent         string // after the number
	// units holds the pattern of each magnitude from 10^3 to 10^14; a
	// magnitude without one is written in full.
	units map[int]compactUnit
}

// maxMagnitude is the largest magnitude with a pattern; larger amounts use it.
const maxMagnitude = 14

// stepsOf gives each unit three magnitudes, from 10^first: 0, 00 and 000.
func stepsOf(first int, sep string, units ...string) map[int]compactUnit {
	m := map[int]compactUnit{}
	for i, u := range units {
		for z := 1; z <= 3; z++ {
			m[first+3*i+z-1] = compactUnit{z, sep + u}
		}
	}
	return m
}

const nbsp = " "

// numberStyles are CLDR's short compact and percent patterns, as Node 26 and
// current browsers have them.
var numberStyles = map[i18n.Locale]numberStyle{
	"en": {decimal: ".", group: ",", percentGrouping: 4, percent: "%", units: stepsOf(3, "", "K", "M", "B", "T")},
	"cs": {decimal: ",", group: nbsp, percentGrouping: 4, percent: nbsp + "%", units: stepsOf(3, nbsp, "tis.", "mil.", "mld.", "bil.")},
	"de": {decimal: ",", group: ".", percentGrouping: 4, percent: nbsp + "%", units: stepsOf(6, nbsp, "Mio.", "Mrd.", "Bio.")},
	"es": {decimal: ",", group: ".", percentGrouping: 5, percent: nbsp + "%", units: func() map[int]compactUnit {
		m := stepsOf(3, nbsp, "mil", "M")
		m[9] = compactUnit{4, nbsp + "M"}
		m[10] = compactUnit{2, nbsp + "mil" + nbsp + "M"}
		m[11] = compactUnit{3, nbsp + "mil" + nbsp + "M"}
		for k, v := range stepsOf(12, nbsp, "B") {
			m[k] = v
		}
		return m
	}()},
	"fr": {decimal: ",", group: " ", percentGrouping: 4, percent: nbsp + "%", units: stepsOf(3, nbsp, "k", "M", "Md", "Bn")},
	"pl": {decimal: ",", group: nbsp, percentGrouping: 5, percent: "%", units: stepsOf(3, nbsp, "tys.", "mln", "mld", "bln")},
	"ru": {decimal: ",", group: nbsp, percentGrouping: 4, percent: nbsp + "%", units: stepsOf(3, nbsp, "тыс.", "млн", "млрд", "трлн")},
	"zh-Hans": {decimal: ".", group: ",", percentGrouping: 4, percent: "%", units: func() map[int]compactUnit {
		m := map[int]compactUnit{}
		for z := 1; z <= 4; z++ {
			m[3+z] = compactUnit{z, "万"}
			m[7+z] = compactUnit{z, "亿"}
		}
		for z := 1; z <= 3; z++ {
			m[11+z] = compactUnit{z, "万亿"}
		}
		return m
	}()},
}

func styleOf(l i18n.Locale) numberStyle {
	if s, ok := numberStyles[l]; ok {
		return s
	}
	return numberStyles[i18n.English]
}

// labelText is a resource label as the page writes it in l.
func labelText(l i18n.Locale, label ResourceLabel) string {
	s := styleOf(l)
	if label.Kind == resourceKindYield {
		return s.yield(label.Value)
	}
	n, err := strconv.ParseInt(label.Value, 10, 64)
	if err != nil {
		return label.Value
	}
	return s.compact(n)
}

// compact writes n as Intl's compact notation does, rounding half away from
// zero to one fraction digit; a rounding that adds a digit takes the next
// magnitude's pattern, so 999,950 is "1M", not "1000K".
func (s numberStyle) compact(n int64) string {
	if n < 0 {
		return "-" + s.compact(-n)
	}
	for mag := len(strconv.FormatInt(n, 10)) - 1; ; mag++ {
		at := min(mag, maxMagnitude)
		u, ok := s.units[at]
		if !ok {
			return s.grouped(strconv.FormatInt(n, 10), 5)
		}
		// Tenths of the unit: n over 10^(at-zeros), rounded.
		d := int64(1)
		for range at - u.zeros {
			d *= 10
		}
		tenths := (n + d/2) / d
		whole := strconv.FormatInt(tenths/10, 10)
		if len(whole) > u.zeros && at < maxMagnitude {
			continue
		}
		out := s.grouped(whole, 5)
		if f := tenths % 10; f != 0 {
			out += s.decimal + strconv.FormatInt(f, 10)
		}
		return out + u.unit
	}
}

// yield writes a fraction, as the label's value holds it, as a whole
// percentage rounded half away from zero.
func (s numberStyle) yield(value string) string {
	r, ok := new(big.Rat).SetString(value)
	if !ok || r.Sign() < 0 {
		return value
	}
	r.Mul(r, big.NewRat(100, 1))
	num := new(big.Int).Mul(r.Num(), big.NewInt(2))
	num.Add(num, r.Denom())
	num.Quo(num, new(big.Int).Mul(r.Denom(), big.NewInt(2)))
	return s.grouped(num.String(), s.percentGrouping) + s.percent
}

// grouped puts the group separator between each three digits, from atLeast
// digits on.
func (s numberStyle) grouped(digits string, atLeast int) string {
	if len(digits) < atLeast {
		return digits
	}
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(s.group)
		}
		b.WriteRune(c)
	}
	return b.String()
}
