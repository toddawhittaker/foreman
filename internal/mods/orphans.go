package mods

import "slices"

// offerOrphans finds each installed mod that a removed mod required and no
// mod left enabled requires, following the chain as removals free more
// (SPEC 9.4). Each is offered ticked; a ticked one is removed in the same
// plan, an unticked (kept) one stays as it is. Built-in, folder and
// optional dependencies are never offered.
func (r *resolver) offerOrphans(kept []string) {
	offered := map[string]bool{}
	for {
		name, of := r.nextOrphan(offered)
		if name == "" {
			return
		}
		offered[name] = true
		keep := slices.Contains(kept, name)
		r.orphans = append(r.orphans, Offer{Name: name, Of: of, Chosen: !keep})
		if !keep {
			t := r.targets[name]
			t.enabled, t.off, t.remove, t.why = false, true, true, Reason{Code: WhyOnlyNeededBy, Of: of}
		}
	}
}

// nextOrphan returns the first orphan not yet offered, by the removed mod's
// name and then its dependency order, and the removed mod that needed it.
func (r *resolver) nextOrphan(offered map[string]bool) (string, string) {
	names := make([]string, 0, len(r.targets))
	for n, t := range r.targets {
		if t.remove {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	for _, n := range names {
		for _, s := range r.targets[n].localDeps {
			d, err := ParseDep(s)
			if err != nil || !d.required() || offered[d.Name] {
				continue
			}
			o := r.targets[d.Name]
			if o == nil || !o.installed || o.builtin || o.local || o.remove || r.requiredByEnabled(d.Name) {
				continue
			}
			return d.Name, n
		}
	}
	return "", ""
}
