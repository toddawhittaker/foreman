package resource

// A patch's label is hidden near a patch of the same resource this many
// times richer, within this many tiles (SPEC 5.6, "Resource amounts").
const (
	hideRatio = 4
	hideReach = 32
)

// HiddenLabels says, for each patch, whether its label is left off the
// picture: a patch of the same resource with at least hideRatio times its
// amount lies within hideReach tiles on both axes, box edge to box edge. A
// spot is measured by its tiles, against spots of its own fluid. The tiles
// count as the patches' gaps do, so touching boxes are 1 apart.
func HiddenLabels(ps []Patch) []bool {
	hidden := make([]bool, len(ps))
	for i, p := range ps {
		for _, q := range ps {
			if q.Name == p.Name && q.Fluid == p.Fluid && size(q) > size(p) && size(q) >= hideRatio*size(p) &&
				boxGap(p.Box[0], p.Box[2], q.Box[0], q.Box[2]) <= hideReach &&
				boxGap(p.Box[1], p.Box[3], q.Box[1], q.Box[3]) <= hideReach {
				hidden[i] = true
				break
			}
		}
	}
	return hidden
}

// boxGap is the distance on one axis between the spans lo1..hi1 and lo2..hi2,
// 0 where they overlap.
func boxGap(lo1, hi1, lo2, hi2 int) int {
	return max(0, lo2-hi1, lo1-hi2)
}

// size is what a label gives: a patch's amount, or a spot's tiles.
func size(p Patch) int64 {
	if p.Fluid {
		return int64(p.Entities)
	}
	return p.Amount
}
