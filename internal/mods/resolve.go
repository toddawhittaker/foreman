package mods

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/world"
)

// maxSteps bounds the greedy walk; real mod sets settle in far fewer.
const maxSteps = 10000

type target struct {
	from        string // installed version
	fromEnabled bool
	installed   bool
	version     string
	enabled     bool
	fixed       bool // version may not change
	builtin     bool
	spaceAge    bool
	local       bool
	off         bool // turned off by this request; never re-enabled
	remove      bool
	newest      bool // prefer the newest release over the installed one
	upload      *Upload
	release     *modportal.Release
	notOnPortal bool
	deps        []Dep
	why         Reason
	warning     string
}

type resolver struct {
	ctx       context.Context
	lookup    Lookup
	line      string
	targets   map[string]*target
	needs     map[string]map[string]Dep // mod -> requirer -> dep
	offers    map[string][]string       // requirer -> optional names
	chosen    map[string]bool
	cache     map[string]*modportal.Mod
	conflicts []Conflict
	queue     []string
}

// Resolve turns a request into a plan, greedily and without backtracking
// (SPEC 9.2): for each required dependency it keeps the installed version
// if it meets every constraint, else takes the newest release on the
// world's game line that meets every constraint collected so far. It
// returns an error only when the portal fails in a way other than not
// having a mod.
func Resolve(ctx context.Context, w World, req Request, lookup Lookup) (Plan, error) {
	r := &resolver{
		ctx: ctx, lookup: lookup, line: gameLine(w.GameVersion),
		targets: map[string]*target{}, needs: map[string]map[string]Dep{},
		offers: map[string][]string{}, chosen: map[string]bool{}, cache: map[string]*modportal.Mod{},
	}
	for _, n := range req.Optional {
		r.chosen[n] = true
	}
	r.addWorld(w)
	var unresolved []Unresolved
	var err error
	switch req.Kind {
	case KindInstall:
		r.install(req.Names)
	case KindUpdate:
		r.update(req.Names)
	case KindEnable:
		r.enable(req.Names)
	case KindDisable, KindRemove:
		r.turnOff(req.Names, req.Kind == KindRemove)
	case KindUpload:
		r.addUpload(req.Upload, Reason{Code: WhyAsked})
	case KindSync:
		unresolved, err = r.sync(req.SaveMods, req.Choices)
	default:
		r.conflict(Conflict{Code: ConflictBadRequest})
	}
	if err != nil {
		return Plan{}, err
	}
	if err := r.walk(); err != nil {
		return Plan{}, err
	}
	r.check()
	return r.plan(unresolved), nil
}

func (r *resolver) addWorld(w World) {
	spaceAge := map[string]bool{}
	for _, n := range world.SpaceAgeMods(w.GameVersion) {
		spaceAge[n] = true
	}
	for _, n := range world.BuiltinMods(w.GameVersion) {
		r.targets[n] = &target{
			installed: true, from: w.GameVersion, version: w.GameVersion, fixed: true,
			builtin: true, spaceAge: spaceAge[n], enabled: !spaceAge[n] || w.SpaceAge,
		}
		r.targets[n].fromEnabled = r.targets[n].enabled
	}
	for _, m := range w.Mods {
		if m.BuiltIn || r.targets[m.Name] != nil {
			continue
		}
		r.targets[m.Name] = &target{
			installed: true, from: m.Version, fromEnabled: m.Enabled, version: m.Version,
			enabled: m.Enabled, local: m.Local, fixed: m.Local,
		}
	}
}

func (r *resolver) conflict(c Conflict) { r.conflicts = append(r.conflicts, c) }

func (r *resolver) install(names []string) {
	for _, n := range names {
		t := r.targets[n]
		switch {
		case t != nil && t.builtin:
			r.conflict(Conflict{Code: ConflictBuiltIn, Mod: n})
		case t != nil:
			t.enabled, t.why = true, Reason{Code: WhyAsked}
		default:
			r.targets[n] = &target{enabled: true, why: Reason{Code: WhyAsked}}
		}
	}
}

func (r *resolver) update(names []string) {
	if len(names) == 0 {
		for n, t := range r.targets {
			if !t.builtin && !t.local {
				names = append(names, n)
			}
		}
		slices.Sort(names)
	}
	for _, n := range names {
		if t := r.installedTarget(n); t != nil {
			if t.local {
				r.conflict(Conflict{Code: ConflictLocal, Mod: n})
				continue
			}
			t.newest, t.why = true, Reason{Code: WhyAsked}
		}
	}
}

func (r *resolver) enable(names []string) {
	for _, n := range names {
		if t := r.installedTarget(n); t != nil {
			t.enabled, t.why = true, Reason{Code: WhyAsked}
		}
	}
}

func (r *resolver) turnOff(names []string, remove bool) {
	for _, n := range names {
		t := r.installedTarget(n)
		if t == nil {
			continue
		}
		if remove && t.local {
			r.conflict(Conflict{Code: ConflictLocal, Mod: n})
			continue
		}
		t.enabled, t.off, t.remove, t.why = false, true, remove, Reason{Code: WhyAsked}
	}
}

// installedTarget returns the installed, non-built-in mod n, or records
// why it cannot be changed and returns nil.
func (r *resolver) installedTarget(n string) *target {
	t := r.targets[n]
	switch {
	case t == nil:
		r.conflict(Conflict{Code: ConflictNotInstalled, Mod: n})
		return nil
	case t.builtin:
		r.conflict(Conflict{Code: ConflictBuiltIn, Mod: n})
		return nil
	}
	return t
}

func (r *resolver) addUpload(u *Upload, why Reason) {
	if u == nil {
		r.conflict(Conflict{Code: ConflictBadRequest})
		return
	}
	t := r.targets[u.Name]
	switch {
	case t != nil && t.builtin:
		r.conflict(Conflict{Code: ConflictBuiltIn, Mod: u.Name})
		return
	case t != nil && t.local:
		r.conflict(Conflict{Code: ConflictLocal, Mod: u.Name})
		return
	case t == nil:
		t = &target{}
		r.targets[u.Name] = t
	}
	t.version, t.fixed, t.enabled, t.upload, t.why = u.Version, true, true, u, why
}

// sync makes the world's mods exactly the save's (SPEC 9.8, 9.10).
func (r *resolver) sync(save []SaveMod, choices map[string]Choice) ([]Unresolved, error) {
	inSave := map[string]bool{}
	var unresolved []Unresolved
	fromSave := Reason{Code: WhyFromSave}
	for _, sm := range save {
		inSave[sm.Name] = true
		t := r.targets[sm.Name]
		if t != nil && t.builtin {
			if t.spaceAge && !t.enabled {
				r.conflict(Conflict{Code: ConflictSpaceAgeOff, Mod: sm.Name})
			}
			continue
		}
		if t != nil && t.installed && t.from == sm.Version {
			t.version, t.fixed, t.enabled, t.why = sm.Version, true, true, fromSave
			continue
		}
		if t != nil && t.local {
			r.conflict(Conflict{Code: ConflictLocal, Mod: sm.Name, Version: sm.Version})
			continue
		}
		mod, err := r.mod(sm.Name)
		if err != nil {
			return nil, err
		}
		if rel := findRelease(mod, sm.Version, r.line); rel != nil {
			r.pin(sm.Name, sm.Version, rel, fromSave, "")
			continue
		}
		u := Unresolved{Name: sm.Name, Version: sm.Version, Nearest: nearest(mod, sm.Version, r.line)}
		if c, ok := choices[sm.Name]; ok {
			u.Choice = &c
		}
		unresolved = append(unresolved, u)
		switch {
		case usableChoice(u) && u.Choice.Kind == ChoiceUpload:
			r.addUpload(u.Choice.Upload, fromSave)
		case usableChoice(u) && u.Choice.Kind == ChoiceNearest:
			warn := ""
			if compareVersions(u.Nearest, sm.Version) < 0 {
				warn = WarnOlderThanSave
			}
			r.pin(sm.Name, u.Nearest, findRelease(mod, u.Nearest, r.line), fromSave, warn)
		case t != nil:
			t.enabled, t.off, t.why = false, true, fromSave
		}
	}
	for n, t := range r.targets {
		if !inSave[n] && !t.builtin && t.enabled {
			t.enabled, t.off, t.why = false, true, Reason{Code: WhyNotInSave}
		}
	}
	return unresolved, nil
}

func (r *resolver) pin(name, version string, rel *modportal.Release, why Reason, warn string) {
	t := r.targets[name]
	if t == nil {
		t = &target{}
		r.targets[name] = t
	}
	t.version, t.release, t.fixed, t.enabled, t.why, t.warning = version, rel, true, true, why, warn
}

// mod looks a mod up once per resolve; nil means the portal lacks it.
func (r *resolver) mod(name string) (*modportal.Mod, error) {
	if m, ok := r.cache[name]; ok {
		return m, nil
	}
	m, err := r.lookup(r.ctx, name)
	if errors.Is(err, modportal.ErrNotOnPortal) {
		r.cache[name] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.cache[name] = &m
	return &m, nil
}

func (r *resolver) push(name string) { r.queue = append(r.queue, name) }

func (r *resolver) walk() error {
	names := make([]string, 0, len(r.targets))
	for n := range r.targets {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		r.push(n)
	}
	for steps := 0; len(r.queue) > 0; steps++ {
		if steps >= maxSteps {
			r.conflict(Conflict{Code: ConflictTooComplex})
			return nil
		}
		n := r.queue[0]
		r.queue = r.queue[1:]
		if err := r.settle(n); err != nil {
			return err
		}
	}
	return nil
}

// settle picks a version for one enabled mod and records its dependencies.
func (r *resolver) settle(name string) error {
	t := r.targets[name]
	if !t.enabled || t.builtin {
		return nil
	}
	if err := r.pick(name, t); err != nil {
		return err
	}
	if err := r.readDeps(name, t); err != nil {
		return err
	}
	for _, need := range r.needs {
		delete(need, name)
	}
	r.offers[name] = nil
	for _, d := range t.deps {
		switch {
		case d.Kind == Incompatible:
			continue
		case !d.required() && !r.chosen[d.Name] && !r.present(d.Name):
			if d.Kind == Optional {
				r.offers[name] = append(r.offers[name], d.Name)
			}
			continue
		}
		why := Reason{Code: WhyRequiredBy, Of: name}
		if !d.required() {
			why.Code = WhyOptionalOf
		}
		if r.needs[d.Name] == nil {
			r.needs[d.Name] = map[string]Dep{}
		}
		r.needs[d.Name][name] = d
		dt := r.targets[d.Name]
		switch {
		case dt == nil:
			r.targets[d.Name] = &target{enabled: true, why: why}
			r.push(d.Name)
		case dt.builtin || dt.off:
			// checked once the walk is done
		default:
			// Mods this change touches, or that it pulls in, are shown in
			// the plan; untouched chains of installed mods are not.
			again := !dt.enabled || (!dt.fixed && (dt.version == "" || !d.Allows(dt.version)))
			if dt.why == (Reason{}) && (again || t.why != (Reason{})) {
				dt.why = why
			}
			if again {
				dt.enabled = true
				r.push(d.Name)
			}
		}
	}
	return nil
}

func (r *resolver) present(name string) bool {
	t := r.targets[name]
	return t != nil && t.enabled
}

func (r *resolver) pick(name string, t *target) error {
	if t.fixed {
		return nil
	}
	ok := func(v string) bool {
		for _, d := range r.needs[name] {
			if !d.Allows(v) {
				return false
			}
		}
		return true
	}
	if t.installed && !t.newest && ok(t.from) {
		t.version, t.release = t.from, nil
		return nil
	}
	mod, err := r.mod(name)
	if err != nil {
		return err
	}
	t.notOnPortal = mod == nil
	t.version, t.release = "", nil
	if mod != nil {
		for i := len(mod.Releases) - 1; i >= 0; i-- {
			rel := mod.Releases[i]
			if rel.FactorioVersion == r.line && ok(rel.Version) {
				if !t.installed || rel.Version != t.from {
					t.release = &rel
				}
				t.version = rel.Version
				break
			}
		}
	}
	if t.version == "" && t.installed && ok(t.from) {
		t.version = t.from
	}
	return nil
}

func (r *resolver) readDeps(name string, t *target) error {
	var raw []string
	switch {
	case t.upload != nil:
		raw = t.upload.Dependencies
	case t.release != nil:
		raw = t.release.Dependencies
	case t.version != "":
		mod, err := r.mod(name)
		if err != nil {
			return err
		}
		if rel := findRelease(mod, t.version, ""); rel != nil {
			raw = rel.Dependencies
		}
	}
	t.deps = t.deps[:0]
	for _, s := range raw {
		d, err := ParseDep(s)
		if err != nil {
			r.conflict(Conflict{Code: ConflictBadDep, Mod: name, Text: s})
			continue
		}
		if d.Name != name {
			t.deps = append(t.deps, d)
		}
	}
	return nil
}

// check finds what the walk could not fix, over the final enabled set.
func (r *resolver) check() {
	names := make([]string, 0, len(r.targets))
	for n := range r.targets {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		t := r.targets[n]
		if !t.enabled {
			continue
		}
		needs := r.activeNeeds(n)
		switch {
		case t.builtin:
		case t.version == "" && t.notOnPortal:
			r.conflict(Conflict{Code: ConflictNotOnPortal, Mod: n})
			continue
		case t.version == "":
			r.conflict(Conflict{Code: ConflictNoRelease, Mod: n, Needs: needs})
			continue
		}
		for _, need := range needs {
			if !(Dep{Op: need.Op, Version: need.Version}).Allows(t.version) {
				r.conflict(Conflict{Code: ConflictVersion, Mod: n, Version: t.version, Needs: needs})
				break
			}
		}
		for _, d := range t.deps {
			dt := r.targets[d.Name]
			switch {
			case d.Kind == Incompatible && dt != nil && dt.enabled:
				r.conflict(Conflict{Code: ConflictIncompatible, Mod: n, Other: d.Name})
			case !d.required() || (dt != nil && dt.enabled):
			case dt != nil && dt.spaceAge:
				r.conflict(Conflict{Code: ConflictSpaceAgeOff, Mod: n, Other: d.Name})
			default:
				r.conflict(Conflict{Code: ConflictRequiredBy, Mod: d.Name, Other: n})
			}
		}
	}
}

// activeNeeds lists the constraints on a mod from enabled mods, by requirer.
func (r *resolver) activeNeeds(name string) []Need {
	var out []Need
	for by, d := range r.needs[name] {
		if r.present(by) && d.Op != "" {
			out = append(out, Need{By: by, Op: d.Op, Version: d.Version})
		}
	}
	slices.SortFunc(out, func(a, b Need) int { return strings.Compare(a.By, b.By) })
	return out
}

func (r *resolver) plan(unresolved []Unresolved) Plan {
	var p Plan
	names := make([]string, 0, len(r.targets))
	for n := range r.targets {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		t := r.targets[n]
		if t.builtin {
			continue
		}
		c := Change{Name: n, From: t.from, To: t.version, Why: t.why, Enabled: t.enabled,
			Release: t.release, Upload: t.upload != nil, Warning: t.warning}
		switch {
		case t.remove:
			c.Action, c.To = ActRemove, ""
		case !t.installed && (!t.enabled || t.version == ""):
			continue
		case !t.installed:
			c.Action = ActInstall
		case t.version != "" && compareVersions(t.version, t.from) > 0:
			c.Action = ActUpgrade
		case t.version != "" && compareVersions(t.version, t.from) < 0:
			c.Action = ActDowngrade
		case t.upload != nil:
			c.Action = ActUpgrade // same version, replaced by the uploaded zip
		case t.enabled && !t.fromEnabled:
			c.Action = ActEnable
		case !t.enabled && t.fromEnabled:
			c.Action = ActDisable
		case t.why == (Reason{}):
			continue
		default:
			c.Action, c.To = ActKeep, t.from
		}
		if c.To == "" && c.Action != ActRemove {
			c.To = t.from
		}
		p.Changes = append(p.Changes, c)
		if c.Action != ActKeep && c.Action != ActDisable && c.Action != ActRemove {
			for _, o := range r.offers[n] {
				p.Optional = append(p.Optional, Offer{Name: o, Of: n})
			}
		}
	}
	p.Conflicts = dedupe(r.conflicts)
	p.Unresolved = unresolved
	slices.SortFunc(p.Unresolved, func(a, b Unresolved) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(p.Optional, func(a, b Offer) int {
		return strings.Compare(a.Name+"\x00"+a.Of, b.Name+"\x00"+b.Of)
	})
	p.Hash = hashPlan(p)
	return p
}

func dedupe(cs []Conflict) []Conflict {
	key := func(c Conflict) string {
		b, _ := json.Marshal(c)
		return string(b)
	}
	slices.SortFunc(cs, func(a, b Conflict) int { return strings.Compare(key(a), key(b)) })
	return slices.CompactFunc(cs, func(a, b Conflict) bool { return key(a) == key(b) })
}

// hashPlan is a stable digest of everything in the plan but the hash, so
// a plan shown and a plan recomputed can be compared.
func hashPlan(p Plan) string {
	p.Hash = ""
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// findRelease returns the release at version, on line when line is set.
func findRelease(mod *modportal.Mod, version, line string) *modportal.Release {
	if mod == nil {
		return nil
	}
	for _, rel := range mod.Releases {
		if rel.Version == version && (line == "" || rel.FactorioVersion == line) {
			return &rel
		}
	}
	return nil
}

// nearest is the lowest release on line above version, else the newest
// below it (plan ruling for SPEC 9.10).
func nearest(mod *modportal.Mod, version, line string) string {
	if mod == nil {
		return ""
	}
	above, below := "", ""
	for _, rel := range mod.Releases {
		if rel.FactorioVersion != line {
			continue
		}
		c := compareVersions(rel.Version, version)
		if c > 0 && (above == "" || compareVersions(rel.Version, above) < 0) {
			above = rel.Version
		}
		if c < 0 && (below == "" || compareVersions(rel.Version, below) > 0) {
			below = rel.Version
		}
	}
	if above != "" {
		return above
	}
	return below
}

// gameLine returns "2.0" for "2.0.72".
func gameLine(gameVersion string) string {
	parts := strings.SplitN(gameVersion, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}
