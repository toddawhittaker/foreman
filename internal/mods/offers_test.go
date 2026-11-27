package mods

import (
	"slices"
	"testing"
)

func offerNames(os []Offer) []string {
	var out []string
	for _, o := range os {
		out = append(out, o.Name)
	}
	return out
}

// An optional mod with no release on the world's line is left out with a
// note, never offered (#970, SPEC 9.2).
func TestOfferWithoutReleaseOnLineUnavailable(t *testing.T) {
	p := portal{
		"black-rubber-belts-remastered": {rel("2.4.6", "2.0", "? AdvancedBeltsSA", "? AdvancedBeltsUpdated")},
		"AdvancedBeltsSA":               {rel("1.0.0", "2.0")},
		"AdvancedBeltsUpdated":          {rel("2.4.0", "1.1")},
	}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"black-rubber-belts-remastered"}})
	if got := offerNames(plan.Optional); !slices.Equal(got, []string{"AdvancedBeltsSA"}) {
		t.Errorf("optional = %v, want [AdvancedBeltsSA]", got)
	}
	if got := offerNames(plan.Unavailable); !slices.Equal(got, []string{"AdvancedBeltsUpdated"}) {
		t.Errorf("unavailable = %v, want [AdvancedBeltsUpdated]", got)
	}
}

// The parent's version limit counts: a release on the line below it does
// not fit.
func TestOfferBelowParentLimitUnavailable(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "? b >= 2.0.0")}, "b": {rel("1.5.0", "2.0")}}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a"}})
	if len(plan.Optional) != 0 || !slices.Equal(offerNames(plan.Unavailable), []string{"b"}) {
		t.Errorf("optional %v, unavailable %v", plan.Optional, plan.Unavailable)
	}
}

// An optional mod whose own required dependencies would conflict is left
// out: "! space-age" on a Space Age world, or needing space-age without it.
func TestOfferConflictingDepsUnavailable(t *testing.T) {
	p := portal{
		"a":      {rel("1.0.0", "2.0", "? nosa", "? needsa", "? fine")},
		"nosa":   {rel("1.0.0", "2.0", "! space-age")},
		"needsa": {rel("1.0.0", "2.0", "space-age")},
		"fine":   {rel("1.0.0", "2.0")},
	}
	sa := resolve(t, p, World{SpaceAge: true}, Request{Kind: KindInstall, Names: []string{"a"}})
	if got := offerNames(sa.Optional); !slices.Equal(got, []string{"fine", "needsa"}) {
		t.Errorf("space age: optional = %v", got)
	}
	if got := offerNames(sa.Unavailable); !slices.Equal(got, []string{"nosa"}) {
		t.Errorf("space age: unavailable = %v", got)
	}
	plain := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a"}})
	if got := offerNames(plain.Optional); !slices.Equal(got, []string{"fine", "nosa"}) {
		t.Errorf("no space age: optional = %v", got)
	}
	if got := offerNames(plain.Unavailable); !slices.Equal(got, []string{"needsa"}) {
		t.Errorf("no space age: unavailable = %v", got)
	}
}

// A mod asked for is not offered back by a mod it offered (#972).
func TestOfferNotOfferedBack(t *testing.T) {
	for _, kind := range []string{"? ", "+ "} {
		p := portal{
			"black-rubber-belts-remastered": {rel("2.4.6", "2.0", "? AdvancedBeltsSA")},
			"AdvancedBeltsSA":               {rel("1.0.0", "2.0", kind+"black-rubber-belts-remastered")},
		}
		plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"black-rubber-belts-remastered"}, Optional: []string{"AdvancedBeltsSA"}})
		want := []Offer{{Name: "AdvancedBeltsSA", Of: "black-rubber-belts-remastered", Chosen: true}}
		if !slices.Equal(plan.Optional, want) {
			t.Errorf("%q: optional = %+v, want %+v", kind, plan.Optional, want)
		}
	}
}

// A mod required by another in the plan is not offered.
func TestOfferRequiredElsewhereNotOffered(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "? lib")}, "z": {rel("1.0.0", "2.0", "lib")}, "lib": {rel("1.0.0", "2.0")}}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a", "z"}})
	if len(plan.Optional) != 0 {
		t.Errorf("optional = %+v", plan.Optional)
	}
}

// Two mods offering the same mod give one row (#972).
func TestOfferSharedOnce(t *testing.T) {
	for _, kind := range []string{"? ", "+ "} {
		p := portal{"a": {rel("1.0.0", "2.0", kind+"d")}, "c": {rel("1.0.0", "2.0", kind+"d")}, "d": {rel("1.0.0", "2.0")}}
		plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a", "c"}})
		if got := offerNames(plan.Optional); !slices.Equal(got, []string{"d"}) {
			t.Errorf("%q: optional = %+v", kind, plan.Optional)
		}
	}
}
