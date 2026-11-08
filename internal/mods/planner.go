package mods

import (
	"context"
	"errors"
	"io/fs"
	"slices"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Worlds is what the planner reads from a world; *world.Manager has it.
type Worlds interface {
	Get(ctx context.Context, id string) (world.Summary, error)
	Mods(id string) ([]world.InstalledMod, error)
	ModsFingerprint(id string) (string, error)
	SaveMods(id, save string) (savefile.Save, error)
	PreStartCheck(id string) (world.PreCheck, error)
}

// Portal is what the planner asks the mod portal; *modportal.Client has it.
type Portal interface {
	Mod(ctx context.Context, name string) (modportal.Mod, error)
	Mods(ctx context.Context, names []string) map[string]modportal.ModResult
}

// Planner gathers a world's mods and the portal's releases into plans.
type Planner struct {
	Worlds Worlds
	Portal Portal
}

// ErrNotApplicable is returned by ToChange for a plan with conflicts or an
// unresolved mod without a usable choice.
var ErrNotApplicable = errors.New("mods: the plan cannot be applied")

// ErrNoStagedUpload is returned by ToChange when the plan uses an uploaded
// zip but no staged file name was given.
var ErrNoStagedUpload = errors.New("mods: the plan uses an upload but none was staged")

// Plan resolves req against the world's mods. The fingerprint is read
// first, so a mods folder that changes while resolving is caught when the
// plan is applied (SPEC 9.14).
func (p *Planner) Plan(ctx context.Context, worldID string, req Request) (Plan, string, error) {
	fp, err := p.Worlds.ModsFingerprint(worldID)
	if err != nil {
		return Plan{}, "", err
	}
	w, err := p.world(ctx, worldID)
	if err != nil {
		return Plan{}, "", err
	}
	plan, err := Resolve(ctx, w, req, p.Portal.Mod)
	if err != nil {
		return Plan{}, "", err
	}
	return plan, fp, nil
}

// SyncRequest builds the sync request for one save of the world: the save
// that loads, chosen by the caller (SPEC 7.11, 9.8).
func (p *Planner) SyncRequest(worldID, save string, choices map[string]Choice) (Request, error) {
	s, err := p.Worlds.SaveMods(worldID, save)
	if err != nil {
		return Request{}, err
	}
	req := Request{Kind: KindSync, Choices: choices}
	for _, m := range s.Mods {
		req.SaveMods = append(req.SaveMods, SaveMod{Name: m.Name, Version: m.Version.String()})
	}
	return req, nil
}

// SaveDrops lists, sorted, the mods the plan turns off or removes (for a
// sync, also those it continues without) that the save which loads uses.
// The game deletes their content from the world, so the admin confirms them
// first (SPEC 9.4, 9.10). Built-in mods follow the Space Age flag and never
// appear. A world with no save, or one Foreman cannot read, drops nothing
// it can name; the pre-start check then still refuses.
func (p *Planner) SaveDrops(ctx context.Context, worldID string, plan Plan) ([]string, error) {
	check, err := p.Worlds.PreStartCheck(worldID)
	if err != nil {
		return nil, err
	}
	if check.Save == "" || slices.ContainsFunc(check.Problems, func(pr world.Problem) bool { return pr.Kind == world.ProblemSaveUnreadable }) {
		return nil, nil
	}
	save, err := p.Worlds.SaveMods(worldID, check.Save)
	if err != nil {
		return nil, err
	}
	w, err := p.world(ctx, worldID)
	if err != nil {
		return nil, err
	}
	inSave := map[string]bool{}
	for _, m := range save.Mods {
		inSave[m.Name] = true
	}
	builtIn := map[string]bool{}
	for _, m := range w.Mods {
		builtIn[m.Name] = m.BuiltIn
	}
	var out []string
	add := func(name string) {
		if inSave[name] && !builtIn[name] && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	for _, c := range plan.Changes {
		if c.Action == ActDisable || c.Action == ActRemove {
			add(c.Name)
		}
	}
	for _, u := range plan.Unresolved {
		if usableChoice(u) && u.Choice.Kind == ChoiceWithout {
			add(u.Name)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (p *Planner) world(ctx context.Context, worldID string) (World, error) {
	s, err := p.Worlds.Get(ctx, worldID)
	if err != nil {
		return World{}, err
	}
	installed, err := p.Worlds.Mods(worldID)
	if err != nil {
		return World{}, err
	}
	return World{GameVersion: s.GameVersion, SpaceAge: s.SpaceAge, Mods: installed}, nil
}

// worldAway reports an error from a world briefly missing or busy, as
// during a restore: a portal check then has nothing to report.
func worldAway(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, world.ErrBusy)
}

// Download is one release the apply operation must fetch and stage.
type Download struct {
	Name    string
	Release modportal.Release
}

// ToChange turns an applicable plan into the releases to download and the
// change to apply. installed is the world's mods when the plan was made;
// staged is the staged file of an uploaded zip, or empty. The caller adds
// the downloads' staged names to change.Add. A removed mod's zip is
// deleted and its mod-list entry turned off. A mod given a new file has
// every other zip of it deleted, so the folder is left with one, whatever
// an earlier change that stopped part-way left behind.
func ToChange(p Plan, installed []world.InstalledMod, staged string) ([]Download, world.ModChange, error) {
	if !p.Applicable() {
		return nil, world.ModChange{}, ErrNotApplicable
	}
	builtIn := map[string]bool{}
	zips := map[string][]string{}
	for _, m := range installed {
		builtIn[m.Name] = builtIn[m.Name] || m.BuiltIn
		if m.File != "" {
			zips[m.Name] = append(zips[m.Name], m.File)
		}
	}
	var downloads []Download
	change := world.ModChange{Enabled: map[string]bool{}}
	for _, c := range p.Changes {
		if builtIn[c.Name] {
			continue // ApplyModsHeld refuses any built-in name
		}
		change.Enabled[c.Name] = c.Enabled
		switch {
		case c.Upload:
			if staged == "" {
				return nil, world.ModChange{}, ErrNoStagedUpload
			}
			change.Add = append(change.Add, staged)
		case c.Release != nil:
			downloads = append(downloads, Download{Name: c.Name, Release: *c.Release})
		}
		replaced := c.Upload || c.Release != nil
		if !replaced && c.Action != ActRemove {
			continue
		}
		// The new zip takes this name, so placing it replaces that file.
		placed := ""
		if replaced {
			placed = world.ModFileName(world.ModInfo{Name: c.Name, Version: c.To})
		}
		for _, file := range zips[c.Name] {
			if file != placed && !slices.Contains(change.Remove, file) {
				change.Remove = append(change.Remove, file)
			}
		}
	}
	return downloads, change, nil
}

// Update is a newer release of an installed mod on the world's game line.
type Update struct {
	Name, From, To string
}

// Updates lists, for each enabled portal mod, the newest release on the
// world's game line when it is newer than the installed one (SPEC 9.3).
// Mods the portal does not have are skipped; any other portal failure is
// returned, never read as "no update". A world briefly missing or busy
// has none.
func (p *Planner) Updates(ctx context.Context, worldID string) ([]Update, error) {
	w, err := p.world(ctx, worldID)
	if worldAway(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	line := GameLine(w.GameVersion)
	mods := checkable(w.Mods)
	results, err := p.fetch(ctx, mods)
	if err != nil {
		return nil, err
	}
	var out []Update
	for _, m := range mods {
		r := results[m.Name]
		if r.Err != nil {
			continue // not on the portal; fetch returns any other error
		}
		if to := newestOnLine(r.Mod, line); to != "" && world.CompareVersions(to, m.Version) > 0 {
			out = append(out, Update{Name: m.Name, From: m.Version, To: to})
		}
	}
	return out, nil
}

// checkable is the enabled mods that are neither built in nor folders.
func checkable(installed []world.InstalledMod) []world.InstalledMod {
	var out []world.InstalledMod
	for _, m := range installed {
		if m.Enabled && !m.BuiltIn && !m.Local {
			out = append(out, m)
		}
	}
	return out
}

// fetch asks the portal for every mod. It returns the first error that is
// not "not on the portal", so an unreachable portal is never mistaken for
// a missing mod.
func (p *Planner) fetch(ctx context.Context, mods []world.InstalledMod) (map[string]modportal.ModResult, error) {
	names := make([]string, 0, len(mods))
	for _, m := range mods {
		names = append(names, m.Name)
	}
	results := p.Portal.Mods(ctx, names)
	for _, n := range names {
		if err := results[n].Err; err != nil && !errors.Is(err, modportal.ErrNotOnPortal) {
			return nil, err
		}
	}
	return results, nil
}

func newestOnLine(m modportal.Mod, line string) string {
	newest := ""
	for _, rel := range m.Releases {
		if rel.FactorioVersion == line && (newest == "" || world.CompareVersions(rel.Version, newest) > 0) {
			newest = rel.Version
		}
	}
	return newest
}
