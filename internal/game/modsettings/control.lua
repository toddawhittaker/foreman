-- Foreman's mod setting reader (SPEC 9.6). When the map is made, it lists
-- every mod setting prototype the world's mods define, in the game's order,
-- and writes them as JSON to script-output/mod-settings-defs.json. It adds no
-- prototypes and has no settings of its own. The same script runs on 1.1,
-- 2.0 and 2.1. A prototype lacks the fields that do not apply to its type
-- (a bool has no minimum), and reading one raises, so each field is read
-- under pcall and left out of the JSON when the game has no value for it.
local value_fields = {
  "default_value", "minimum_value", "maximum_value", "allowed_values",
  "hidden", "allow_blank", "auto_trim",
}

-- read returns proto[field], or nil when the field does not exist.
local function read(proto, field)
  local ok, value = pcall(function() return proto[field] end)
  if ok then
    return value
  end
  return nil
end

-- plain_color turns a color into {r, g, b, a} with names, whichever way the
-- game reports it: by name, or as an array.
local function plain_color(c)
  if type(c) ~= "table" then
    return c
  end
  return {
    r = c.r or c[1] or 0,
    g = c.g or c[2] or 0,
    b = c.b or c[3] or 0,
    a = c.a or c[4] or 1,
  }
end

local function plain(setting_type, value)
  if setting_type == "color-setting" then
    return plain_color(value)
  end
  return value
end

script.on_init(function()
  -- 1.1 has no helpers; its game, which exists only once events run, has
  -- write_file and table_to_json. It has game.mod_setting_prototypes, where
  -- 2.x has prototypes.mod_setting.
  local h = helpers or game
  local protos
  if prototypes then
    protos = prototypes.mod_setting
  else
    protos = game.mod_setting_prototypes
  end

  local list = {}
  for name, proto in pairs(protos) do
    local setting = {
      name = read(proto, "name") or name,
      mod = read(proto, "mod"),
      type = read(proto, "type"),
      setting_type = read(proto, "setting_type"),
      order = read(proto, "order"),
    }
    for _, field in ipairs(value_fields) do
      local value = read(proto, field)
      if value ~= nil then
        if field == "allowed_values" and type(value) == "table" then
          local values = {}
          for i, v in ipairs(value) do
            values[i] = plain(setting.type, v)
          end
          value = values
        elseif field ~= "hidden" and field ~= "allow_blank" and field ~= "auto_trim" then
          value = plain(setting.type, value)
        end
        setting[field] = value
      end
    end
    list[#list + 1] = setting
  end

  h.write_file("mod-settings-defs.json", h.table_to_json({ settings = list }), false)
end)
