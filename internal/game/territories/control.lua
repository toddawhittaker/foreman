-- Foreman's territory reader (SPEC 5.6). When the map is made, it generates
-- Vulcanus's chunks for the picture's area, reads the demolisher
-- territories that reach into it, and writes them as JSON to
-- script-output/territories.json. It adds no prototypes, so the map is the
-- one the preview draws. radius.lua, written beside it for each run, returns
-- the picture's half width in chunks.
local radius = require("radius")

script.on_init(function()
  local planet = game.planets["vulcanus"]
  local surface = planet.surface or planet.create_surface()
  surface.request_to_generate_chunks({ 0, 0 }, radius)
  surface.force_generate_chunk_requests()

  local territories = {}
  for _, t in pairs(surface.get_territories()) do
    -- The picture shows chunks -radius to radius-1 on each axis.
    local chunks = {}
    for _, c in pairs(t.get_chunks()) do
      if c.x >= -radius and c.x < radius and c.y >= -radius and c.y < radius then
        chunks[#chunks + 1] = { c.x, c.y }
      end
    end
    if #chunks > 0 then
      -- A territory has no size of its own; its demolisher's name gives it.
      local unit = ""
      local units = t.get_segmented_units()
      if units[1] then
        unit = units[1].prototype.name
      end
      territories[#territories + 1] = { unit = unit, chunks = chunks }
    end
  end

  -- table_to_json writes an empty table as an object, so none is written by hand.
  local json = '{"territories":[]}'
  if #territories > 0 then
    json = helpers.table_to_json({ territories = territories })
  end
  helpers.write_file("territories.json", json, false)
end)
