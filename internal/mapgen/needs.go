package mapgen

// NeededResources returns the Nauvis autoplace controls a world cannot do
// without, so the form can warn when one is switched off (SPEC 5.6). The names
// are the same on every line. A mod may supply a resource, so a warning is
// advice, not a refusal.
//
//   - iron-ore, copper-ore and stone: the first machines, furnaces, belts and
//     rails are made of them.
//   - coal: power, explosives, and plastic on every line.
//   - crude-oil: plastic and sulfur, which circuits and rockets need.
//   - uranium-ore is left out: nothing on the way to launching a rocket needs it.
//
// With Space Age every warning stays, because reaching space comes first and
// needs all of these. Space Age therefore removes none, and the answer is the
// same with or without it.
func NeededResources(line string, spaceAge bool) []string {
	return []string{"iron-ore", "copper-ore", "stone", "crude-oil", "coal"}
}
