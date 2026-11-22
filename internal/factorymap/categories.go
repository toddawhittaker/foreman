package factorymap

// CategoryOf is the category of an entity from the engine's prototype type,
// so a modded entity lands in one too (SPEC 5.16). A type it does not know is
// CategoryOther. Task F3 builds the table.
func CategoryOf(entityType string) Category { return CategoryOther }
