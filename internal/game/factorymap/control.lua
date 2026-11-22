-- Foreman's factory map reader (SPEC 5.16, 8.16, 14.6; ADR 0030). It runs in
-- a one-off benchmark run of a copy of a save, with no network. It writes, to
-- script-output/factorymap/, the player force's chart and buildings:
--
--   manifest.json              format 1, written last, so a run that stops
--                              early leaves no manifest and Foreman refuses it
--   surface-<i>.jsonl          one {"x","y","data"} line per charted chunk:
--                              x and y are the chunk's position, data is
--                              helpers.encode_string of
--                              force.get_chunk_chart (the game's own chart of
--                              that chunk, 32 by 32 pixels)
--   surface-<i>-entities.jsonl first line: the list of entity prototype types;
--                              then one [type_index, x1, y1, x2, y2] line per
--                              building. The type index counts from 0. The box
--                              is in whole tiles counted from the chart's
--                              top-left corner (the minimum chunk of the
--                              manifest times 32), x1 and y1 included, x2 and
--                              y2 not.
--
-- The limits are SPEC 14.6's, and the Go parser (internal/factorymap) refuses
-- anything above them. The map is never changed: the mod only reads. It
-- reads the player force, and every figure is a whole number.

local OUT = "factorymap/"
local FORCE_NAME = "player"
local CHUNK = 32

-- SPEC 14.6. MAX_BYTES is kept below the 512 MiB limit so the manifest and a
-- partial last line still fit; the chart stops first, so buildings keep room.
local MAX_BYTES = 480 * 1024 * 1024
local MAX_CHART_BYTES = 360 * 1024 * 1024
local MAX_SURFACES = 300
local MAX_CHUNKS_PER_SURFACE = 65536
local MAX_ENTITIES = 2000000

-- Types that are not a building: they come and go, or are not part of the
-- factory.
local SKIP_TYPES = {
  ["character"] = true,
  ["cargo-pod"] = true,
  ["entity-ghost"] = true,
  ["tile-ghost"] = true,
  ["item-request-proxy"] = true,
  ["corpse"] = true,
  ["character-corpse"] = true,
  ["item-entity"] = true,
  ["highlight-box"] = true,
  ["smoke-with-trigger"] = true,
}

-- Output is buffered, since write_file opens the file on every call.
local FLUSH_AT = 256 * 1024
local buffers = {}
local sizes = {}
local total_bytes = 0

local function flush(file)
  local buf = buffers[file]
  if buf and #buf > 0 then
    helpers.write_file(OUT .. file, table.concat(buf), true)
    buffers[file] = {}
    sizes[file] = 0
  end
end

local function emit(file, text)
  local buf = buffers[file]
  if not buf then
    buf = {}
    buffers[file] = buf
    sizes[file] = 0
    -- Start the file empty, so a leftover from an earlier run cannot join in.
    helpers.write_file(OUT .. file, "", false)
  end
  buf[#buf + 1] = text
  sizes[file] = sizes[file] + #text
  total_bytes = total_bytes + #text
  if sizes[file] >= FLUSH_AT then
    flush(file)
  end
end

local function surface_kind(s)
  if s.platform then
    return "platform"
  end
  if s.planet then
    return "planet"
  end
  return "surface"
end

-- Planets first, then the rest by index, so a game with many space platforms
-- hits the surface limit on the platforms.
local function ordered_surfaces()
  local planets, others = {}, {}
  for _, s in pairs(game.surfaces) do
    if surface_kind(s) == "planet" then
      planets[#planets + 1] = s
    else
      others[#others + 1] = s
    end
  end
  local by_index = function(a, b)
    return a.index < b.index
  end
  table.sort(planets, by_index)
  table.sort(others, by_index)
  for _, s in ipairs(others) do
    planets[#planets + 1] = s
  end
  return planets
end

-- write_chart writes the chart file of one surface and returns the chunk count
-- and the inclusive chunk bounds, or nil when no chunk is charted.
local function write_chart(force, s, file)
  local count = 0
  local min_x, min_y, max_x, max_y
  for c in s.get_chunks() do
    if count >= MAX_CHUNKS_PER_SURFACE or total_bytes >= MAX_CHART_BYTES then
      break
    end
    local pos = { c.x, c.y }
    if force.is_chunk_charted(s, pos) then
      local ok, data = pcall(function()
        return force.get_chunk_chart(s, pos)
      end)
      if ok and type(data) == "string" then
        local encoded = helpers.encode_string(data)
        if encoded then
          emit(file, '{"x":' .. string.format("%d", c.x) .. ',"y":' .. string.format("%d", c.y)
            .. ',"data":"' .. encoded .. '"}\n')
          count = count + 1
          if not min_x or c.x < min_x then min_x = c.x end
          if not max_x or c.x > max_x then max_x = c.x end
          if not min_y or c.y < min_y then min_y = c.y end
          if not max_y or c.y > max_y then max_y = c.y end
        end
      end
    end
  end
  flush(file)
  if count == 0 then
    return nil
  end
  return count, min_x, min_y, max_x, max_y
end

local function corner(point, axis)
  return point[axis] or point[axis == "x" and 1 or 2]
end

-- write_entities writes the buildings of the force on one surface, in tiles
-- from the chart's corner, and returns how many it wrote, and whether the
-- limit stopped it. entities_left is what the whole run may still write.
local function write_entities(force, s, file, min_x, min_y, max_x, max_y, entities_left)
  local origin_x, origin_y = min_x * CHUNK, min_y * CHUNK
  local width, height = (max_x - min_x + 1) * CHUNK, (max_y - min_y + 1) * CHUNK
  local type_index, types = {}, {}
  local lines = {}
  local written, truncated = 0, false
  for _, e in pairs(s.find_entities_filtered({ force = force })) do
    local t = e.type
    if not SKIP_TYPES[t] and e.valid then
      if written >= entities_left or total_bytes >= MAX_BYTES then
        truncated = true
        break
      end
      local bb = e.bounding_box
      local lt, rb = bb.left_top, bb.right_bottom
      -- Round out to whole tiles, and keep what falls in the chart.
      local x1 = math.floor(corner(lt, "x")) - origin_x
      local y1 = math.floor(corner(lt, "y")) - origin_y
      local x2 = math.ceil(corner(rb, "x")) - origin_x
      local y2 = math.ceil(corner(rb, "y")) - origin_y
      if x2 <= x1 then x2 = x1 + 1 end
      if y2 <= y1 then y2 = y1 + 1 end
      if x1 < 0 then x1 = 0 end
      if y1 < 0 then y1 = 0 end
      if x2 > width then x2 = width end
      if y2 > height then y2 = height end
      if x1 < x2 and y1 < y2 then
        local i = type_index[t]
        if i == nil then
          i = #types
          type_index[t] = i
          types[#types + 1] = t
        end
        lines[#lines + 1] = "[" .. string.format("%d,%d,%d,%d,%d", i, x1, y1, x2, y2) .. "]\n"
        written = written + 1
      end
    end
  end
  -- The type list comes first, and is only known now.
  local list = "[]"
  if #types > 0 then
    list = helpers.table_to_json(types)
  end
  emit(file, list .. "\n")
  for i = 1, #lines do
    emit(file, lines[i])
    -- Lines are passed on in turn so memory stays low for a large surface.
    lines[i] = false
  end
  flush(file)
  return written, truncated
end

local function read_factory()
  local force = game.forces[FORCE_NAME]
  local manifest_surfaces = {}
  local entities_left = MAX_ENTITIES
  for _, s in ipairs(ordered_surfaces()) do
    if #manifest_surfaces >= MAX_SURFACES then
      break
    end
    local chart_file = "surface-" .. string.format("%d", s.index) .. ".jsonl"
    local entity_file = "surface-" .. string.format("%d", s.index) .. "-entities.jsonl"
    local count, min_x, min_y, max_x, max_y = write_chart(force, s, chart_file)
    if count then
      local total, truncated = write_entities(force, s, entity_file, min_x, min_y, max_x, max_y, entities_left)
      entities_left = entities_left - total
      manifest_surfaces[#manifest_surfaces + 1] = {
        index = s.index,
        name = s.name,
        kind = surface_kind(s),
        chunks = count,
        min_x = min_x,
        min_y = min_y,
        max_x = max_x,
        max_y = max_y,
        chart = chart_file,
        entities = entity_file,
        entities_total = total,
        entities_truncated = truncated,
      }
    end
  end
  -- table_to_json writes an empty list as an object, so none is written by
  -- hand.
  local surfaces = "[]"
  if #manifest_surfaces > 0 then
    surfaces = helpers.table_to_json(manifest_surfaces)
  end
  local manifest = '{"format":1,"game_version":' .. helpers.table_to_json({ script.active_mods["base"] or "" }):sub(2, -2)
    .. ',"tick":' .. string.format("%d", game.tick)
    .. ',"surfaces":' .. surfaces .. "}"
  helpers.write_file(OUT .. "manifest.json", manifest, false)
end

local function run_once()
  if storage.foreman_factorymap_done then
    return
  end
  storage.foreman_factorymap_done = true
  read_factory()
end

-- A mod added to a save runs on_init. The first tick covers a save that
-- already holds the mod.
script.on_init(run_once)
script.on_event(defines.events.on_tick, run_once)
