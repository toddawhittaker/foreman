-- The planet exists only when the startup setting is true, so a world's
-- saved mod-settings.dat changes which planets the game lists.
if settings.startup["fmsetplanet-add"].value then
  data:extend({
    {
      type = "planet",
      name = "fmsetplanet-planet",
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
end
