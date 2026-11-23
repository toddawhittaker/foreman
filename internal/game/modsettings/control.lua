-- Foreman's mod setting reader (SPEC 9.6). When the map is made, it lists
-- every mod setting prototype the world's mods define, in the game's order,
-- and writes them as JSON to script-output/mod-settings-defs.json. It has no
-- settings of its own. It also writes the world's planets, autoplace controls
-- and resources to script-output/planets.json, in the shape
-- internal/mapgen/catalogue.go reads (SPEC 5.6); the star map's space
-- locations and routes to starmap.json, in the shape internal/mapgen/starmap.go
-- reads; and the icon paths its data-final-fixes.lua found to icons.json, in
-- the shape internal/modsettings/iconindex.go reads (ADR 0036). That data
-- stage adds one invisible prototype, a mod-data named foreman-icon-export;
-- the mod adds no other. Each extra file is written under its own pcall, so a
-- failure loses that file only, never the settings. Each setting carries its
-- localised name and description as the game holds them. The same script
-- runs on 1.1, 2.0 and 2.1. A prototype lacks the fields that do not apply to its type
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

-- by_order sorts prototypes as the game lists them: by order, then name.
local function by_order(a, b)
  if a.order ~= b.order then
    return a.order < b.order
  end
  return a.name < b.name
end

-- planet_file lists the planets (none on 1.1, which has one surface), every
-- autoplace control and every resource entity. On 2.x a resource rarely
-- names its control, so each planet lists the resources it places.
local function planet_file()
  local controls_proto, resources_proto
  if prototypes then
    controls_proto = prototypes.autoplace_control
    resources_proto = prototypes.get_entity_filtered({ { filter = "type", type = "resource" } })
  else
    controls_proto = game.autoplace_control_prototypes
    resources_proto = game.get_filtered_entity_prototypes({ { filter = "type", type = "resource" } })
  end

  local controls = {}
  for name, c in pairs(controls_proto) do
    controls[#controls + 1] = {
      name = name,
      category = read(c, "category"),
      richness = read(c, "richness") or false,
      can_be_disabled = read(c, "can_be_disabled") or false,
      order = read(c, "order") or "",
      localised_name = read(c, "localised_name"),
    }
  end
  table.sort(controls, by_order)

  local resources, is_resource = {}, {}
  for name, e in pairs(resources_proto) do
    is_resource[name] = true
    local spec = read(e, "autoplace_specification")
    local control = spec and read(spec, "control") or ""
    resources[#resources + 1] = {
      name = name,
      control = control,
      localised_name = read(e, "localised_name"),
      order = read(e, "order") or "",
    }
  end
  table.sort(resources, by_order)
  for _, r in ipairs(resources) do
    r.order = nil
  end

  -- 2.x has no prototypes.planet: planets are the space locations of type
  -- planet.
  local planets = {}
  local locations = prototypes and read(prototypes, "space_location") or nil
  for name, p in pairs(locations or {}) do
    if read(p, "type") == "planet" then
      local mgs = read(p, "map_gen_settings")
      local planet = {
        name = name,
        localised_name = read(p, "localised_name"),
        order = read(p, "order") or "",
        autoplace_controls = {},
        resources = {},
      }
      if mgs then
        for control, v in pairs(mgs.autoplace_controls or {}) do
          planet.autoplace_controls[control] = { frequency = v.frequency, size = v.size, richness = v.richness }
        end
        local entities = mgs.autoplace_settings and mgs.autoplace_settings.entity
        for entity in pairs(entities and entities.settings or {}) do
          if is_resource[entity] then
            planet.resources[#planet.resources + 1] = entity
          end
        end
        table.sort(planet.resources)
      end
      planets[#planets + 1] = planet
    end
  end
  table.sort(planets, by_order)
  for _, p in ipairs(planets) do
    p.order = nil
  end

  return { planets = planets, autoplace_controls = controls, resources = resources }
end

-- The mod-data prototype data-final-fixes.lua fills; nil when the game has
-- no mod-data (1.1, or a 2.0 image without the type).
local function icon_export()
  local all = prototypes and read(prototypes, "mod_data")
  local export = all and all["foreman-icon-export"]
  local d = export and read(export, "data")
  if type(d) ~= "table" then
    return nil
  end
  return d
end

-- icon_file lists the exported icons with the active mods and their
-- versions, which the icon paths name.
local function icon_file(export)
  return { mods = script.active_mods, icons = export.icons or {} }
end

-- The star map's limits match internal/mapgen/starmap.go, so the file is
-- never refused whole for its counts: past them the rest is left out.
local max_locations = 256
local max_routes = 1024

-- starmap_file lists the space locations as the game places them, in the
-- game's order, and the routes between them. A hidden location, or a
-- parameter prototype, is marked hidden; Foreman leaves it out with its
-- routes. A location is marked as having a starmap icon only when the icon
-- export holds one Foreman may serve.
local function starmap_file(export)
  local parents = export and export.parents or {}
  local has_icon = {}
  for _, icon in ipairs(export and export.icons or {}) do
    if icon.kind == "starmap" then
      has_icon[icon.name] = true
    end
  end
  local locations = {}
  for name, p in pairs(read(prototypes, "space_location") or {}) do
    locations[#locations + 1] = {
      name = name,
      localised_name = read(p, "localised_name"),
      order = read(p, "order") or "",
      distance = read(p, "distance") or 0,
      orientation = read(p, "orientation") or 0,
      magnitude = read(p, "magnitude"),
      label_orientation = read(p, "label_orientation"),
      parent = parents[name],
      hidden = (read(p, "hidden") or read(p, "parameter")) and true or false,
      starmap_icon = has_icon[name] or false,
    }
  end
  table.sort(locations, by_order)
  for i = #locations, max_locations + 1, -1 do
    locations[i] = nil
  end
  local kept = {}
  for _, l in ipairs(locations) do
    l.order = nil
    kept[l.name] = true
  end

  local connections = {}
  for name, c in pairs(read(prototypes, "space_connection") or {}) do
    local from, to = read(c, "from"), read(c, "to")
    from, to = from and read(from, "name"), to and read(to, "name")
    if kept[from] and kept[to] then
      connections[#connections + 1] = {
        name = name, order = read(c, "order") or "",
        from = from, to = to, length = read(c, "length") or 0,
      }
    end
  end
  table.sort(connections, by_order)
  local routes = {}
  for i = 1, math.min(#connections, max_routes) do
    local c = connections[i]
    routes[i] = { from = c.from, to = c.to, length = c.length }
  end
  return { locations = locations, routes = routes }
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
    -- The localised name and description: a key and parameters, or a plain
    -- string. Foreman resolves them against the mods' locale files.
    setting.localised_name = read(proto, "localised_name")
    setting.localised_description = read(proto, "localised_description")
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
  -- The planet list is extra: a failure here must not lose the settings.
  pcall(function()
    h.write_file("planets.json", h.table_to_json(planet_file()), false)
  end)
  -- The icons and the star map are extra too, each on its own. Neither
  -- exists on 1.1, which has no prototypes table.
  if prototypes then
    local ok, export = pcall(icon_export)
    if not ok then
      export = nil
    end
    if export then
      pcall(function()
        h.write_file("icons.json", h.table_to_json(icon_file(export)), false)
      end)
    end
    pcall(function()
      h.write_file("starmap.json", h.table_to_json(starmap_file(export)), false)
    end)
  end
end)
