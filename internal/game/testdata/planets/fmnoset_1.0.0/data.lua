-- A planet and nothing else: the mod defines no setting, so the game writes
-- no mod-settings.dat for a world that loads it.
data:extend({
  {
    type = "planet",
    name = "fmnoset-planet",
    icon = "__base__/graphics/icons/nauvis.png",
    starmap_icon = "__base__/graphics/icons/starmap-planet-nauvis.png",
    starmap_icon_size = 512,
    gravity_pull = 10,
    distance = 25,
    orientation = 0.4,
    magnitude = 1,
    solar_power_in_space = 300,
    map_gen_settings = { autoplace_controls = {}, autoplace_settings = {} },
  },
})
