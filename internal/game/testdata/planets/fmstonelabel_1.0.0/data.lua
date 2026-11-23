-- A control whose name is the base game's own text, as planet mods often do
-- for a shared resource, and a planet that uses it. The mod has no locale
-- file and no setting.
data:extend({
  {
    type = "autoplace-control",
    name = "fmstone-stone",
    localised_name = { "entity-name.stone" },
    order = "z-a",
    category = "resource",
    richness = true,
  },
  {
    type = "planet",
    name = "fmstone-planet",
    icon = "__base__/graphics/icons/nauvis.png",
    starmap_icon = "__base__/graphics/icons/starmap-planet-nauvis.png",
    starmap_icon_size = 512,
    gravity_pull = 10,
    distance = 25,
    orientation = 0.4,
    magnitude = 1,
    solar_power_in_space = 300,
    map_gen_settings = { autoplace_controls = { ["fmstone-stone"] = {} }, autoplace_settings = {} },
  },
})
