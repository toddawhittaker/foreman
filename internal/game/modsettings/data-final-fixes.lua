-- Foreman's icon export (SPEC 9.6, 14.6, ADR 0036). The game's runtime API
-- has no icon paths, so this data stage copies them out of data.raw into one
-- invisible mod-data prototype, which control.lua writes to icons.json. The
-- mod's info.json takes an optional dependency on every world mod, so this
-- runs after their own data-final-fixes. It keeps only icons Foreman may
-- serve: one plain picture under a mod that is not one of the game's own. It
-- also keeps each space location's starmap icon and the parent a PlanetsLib
-- moon names. Everything runs under pcall: a failure here leaves no icons,
-- never a failed run. 1.1 and a 2.0 image without mod-data get no file.

-- The mod-data prototype's name; control.lua reads the same one.
local export_name = "foreman-icon-export"

-- The limits match internal/modsettings/iconindex.go, so the file is never
-- refused whole for its size: past them the rest is left out.
local max_entries = 20000
local max_path = 512
local max_bytes = 3 * 1024 * 1024

-- The game's own mods: their art is Wube's and is never served. 2.1 adds
-- recycler.
local built_in = {
  ["__base__"] = true, ["__core__"] = true, ["__space-age__"] = true,
  ["__quality__"] = true, ["__elevated-rails__"] = true, ["__recycler__"] = true,
}

-- The kinds an object tag names, each with the prototype types under it. A
-- kind defines.prototypes knows lists every subtype it has (every item type
-- under "item"); the others are their own one type.
local kinds = {
  "item", "fluid", "recipe", "technology", "entity", "tile", "virtual-signal",
  "space-location", "quality", "achievement", "item-group", "equipment",
  "asteroid-chunk",
}

-- types_of returns the data.raw types of a kind.
local function types_of(kind)
  local list = {}
  local known = defines.prototypes[kind]
  if type(known) == "table" then
    for t in pairs(known) do
      list[#list + 1] = t
    end
  end
  if #list == 0 then
    list[1] = kind
  end
  table.sort(list)
  return list
end

-- servable returns path when it names a picture under a mod that is not the
-- game's own, else nil.
local function servable(path)
  if type(path) ~= "string" or #path > max_path or path:sub(-4) ~= ".png" then
    return nil
  end
  local mod = path:match("^(__.-__)/")
  if not mod or built_in[mod] then
    return nil
  end
  return path
end

-- one_picture returns the path and size of an icon of one untinted layer: a
-- plain icon, or icons with a single layer that has no tint, shift, scale or
-- floating. Anything else is nil, and the page shows the object's name.
local function one_picture(icon, icons, size, default_size)
  if icon ~= nil then
    return icon, size or default_size
  end
  if type(icons) ~= "table" or #icons ~= 1 then
    return nil
  end
  local layer = icons[1]
  if type(layer) ~= "table" or layer.tint or layer.shift or layer.scale or layer.floating then
    return nil
  end
  -- A layer's size defaults to 64 whatever the prototype's own default is.
  return layer.icon, layer.icon_size or 64
end

local function export()
  local icons, parents, orbits = {}, {}, {}
  local bytes = 0
  local function add(kind, name, path, size)
    path = servable(path)
    if not path or type(name) ~= "string" or type(size) ~= "number" then
      return
    end
    local cost = #kind + #name + #path + 48
    if #icons >= max_entries or bytes + cost > max_bytes then
      return
    end
    bytes = bytes + cost
    icons[#icons + 1] = { kind = kind, name = name, path = path, size = size }
  end

  for _, kind in ipairs(kinds) do
    -- A technology's own icon defaults to 256 pixels, every other to 64.
    local default_size = kind == "technology" and 256 or 64
    for _, t in ipairs(types_of(kind)) do
      for name, proto in pairs(data.raw[t] or {}) do
        if type(proto) == "table" then
          local path, size = one_picture(proto.icon, proto.icons, proto.icon_size, default_size)
          add(kind, name, path, size)
          if kind == "space-location" then
            path, size = one_picture(proto.starmap_icon, proto.starmap_icons, proto.starmap_icon_size, 64)
            add("starmap", name, path, size)
            -- PlanetsLib keeps the body a moon orbits in orbit.parent.
            local orbit = proto.orbit
            local parent = type(orbit) == "table" and orbit.parent
            if type(parent) == "table" and type(parent.name) == "string" then
              parents[name] = parent.name
              -- The offset from the parent lets Foreman rebuild a parent
              -- PlanetsLib deletes because it is only a picture (sprite_only).
              if type(orbit.distance) == "number" and type(orbit.orientation) == "number" then
                orbits[name] = { distance = orbit.distance, orientation = orbit.orientation }
              end
            end
          end
        end
      end
    end
  end
  return { icons = icons, parents = parents, orbits = orbits }
end

-- The mod-data type exists from 2.0 on; without it the run writes no icons.
if type(defines) == "table" and type(defines.prototypes) == "table" and defines.prototypes["mod-data"] then
  pcall(function()
    data:extend({ { type = "mod-data", name = export_name, data = export() } })
  end)
end
