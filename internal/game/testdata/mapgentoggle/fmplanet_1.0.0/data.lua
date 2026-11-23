-- A test planet with its own cliff control and enemy control, both of which
-- the game lets you disable (SPEC 5.6, DESIGN "Cliffs off").
data:extend({
  {type = "autoplace-control", name = "fmplanet_cliff", category = "cliff", order = "z-a", can_be_disabled = true},
  {type = "autoplace-control", name = "fmplanet_enemy", category = "enemy", order = "z-b", can_be_disabled = true},
})

local spawner = table.deepcopy(data.raw["unit-spawner"]["biter-spawner"])
spawner.name = "fmplanet-spawner"
-- As the game's own enemy autoplace does, the probability reads the control's
-- size, so size 0 places none.
spawner.autoplace = {control = "fmplanet_enemy", order = "z", force = "enemy",
  probability_expression = "0.002 * min(1, control:fmplanet_enemy:size)"}
data:extend({spawner})

local planet = table.deepcopy(data.raw.planet.nauvis)
planet.name = "fmplanet"
planet.order = "z"
local mgs = planet.map_gen_settings
mgs.cliff_settings.control = "fmplanet_cliff"
mgs.autoplace_controls = {fmplanet_cliff = {}, fmplanet_enemy = {}}
mgs.autoplace_settings = mgs.autoplace_settings or {}
mgs.autoplace_settings.entity = {settings = {["fmplanet-spawner"] = {}}}
data:extend({planet})
