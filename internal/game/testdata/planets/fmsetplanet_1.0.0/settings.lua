-- A startup setting that, when true, adds a planet (see data.lua).
data:extend({
  {
    type = "bool-setting",
    name = "fmsetplanet-add",
    setting_type = "startup",
    default_value = false,
  },
})
