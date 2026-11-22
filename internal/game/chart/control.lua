-- Foreman's real-game picture (SPEC 5.6, 14.3, 14.6). It runs under
-- --test-save on a throwaway map, which loads it with a player, so charting
-- works. On the first tick it creates one planet's surface, so the mods' own
-- surface and chunk scripts run, generates and charts the chunks from -radius
-- to radius-1 on each axis, and writes the chart to script-output/chart/:
--
--   surface-<i>.jsonl  one {"x","y","data"} line per charted chunk: data is
--                      helpers.encode_string of force.get_chunk_chart
--   manifest.json      the factory map's format 1 with one surface whose
--                      bounds are the whole square, written last, so a run
--                      that stops early leaves no manifest
--
-- The manifest names the surface's entities file as the format requires;
-- this mod writes none and Foreman reads none. params.lua, written beside
-- this file for each run, returns {radius = N, planet = "<name>"}.
local params = require("params")
local radius, planet = params.radius, params.planet

local OUT = "chart/"
-- Ticks for the mods' on_chunk_generated scripts to run before charting.
local SETTLE = 120
-- Ticks after charting starts before the run gives up and writes nothing.
local CAP = 3000

local function chunk_complete(force, surface)
  for cy = -radius, radius - 1 do
    for cx = -radius, radius - 1 do
      if not force.get_chunk_chart(surface, { x = cx, y = cy }) then
        return false
      end
    end
  end
  return true
end

local function export(force, surface)
  local file = "surface-" .. string.format("%d", surface.index) .. ".jsonl"
  local lines, count = {}, 0
  for cy = -radius, radius - 1 do
    for cx = -radius, radius - 1 do
      local px = force.get_chunk_chart(surface, { x = cx, y = cy })
      if px then
        count = count + 1
        lines[#lines + 1] = '{"x":' .. string.format("%d", cx) .. ',"y":' .. string.format("%d", cy)
          .. ',"data":"' .. helpers.encode_string(px) .. '"}\n'
      end
    end
  end
  helpers.write_file(OUT .. file, table.concat(lines), false)
  local manifest = helpers.table_to_json({
    format = 1,
    game_version = script.active_mods["base"] or "",
    tick = game.tick,
    surfaces = { {
      index = surface.index,
      name = surface.name,
      kind = "planet",
      chunks = count,
      min_x = -radius,
      min_y = -radius,
      max_x = radius - 1,
      max_y = radius - 1,
      chart = file,
      entities = "surface-" .. string.format("%d", surface.index) .. "-entities.jsonl",
      entities_total = 0,
      entities_truncated = false,
    } },
  })
  helpers.write_file(OUT .. "manifest.json", manifest, false)
end

local function finish(passed)
  storage.finished = true
  pcall(function() script.finish_test(passed) end)
end

script.on_event(defines.events.on_tick, function()
  if storage.finished then
    return
  end
  local force = game.forces.player
  if not storage.surface then
    local p = game.planets[planet]
    if not p then
      log("[foreman-chart] no planet " .. planet)
      return finish(false)
    end
    local surface = p.surface
    if not surface then
      local ok, err = pcall(function() surface = p.create_surface() end)
      if not ok or not surface then
        log("[foreman-chart] could not create the surface of " .. planet .. ": " .. tostring(err))
        return finish(false)
      end
    end
    storage.surface = surface.name
    storage.start_tick = game.tick
    surface.request_to_generate_chunks({ 0, 0 }, radius)
    surface.force_generate_chunk_requests()
    return
  end
  local surface = game.get_surface(storage.surface)
  if not surface then
    return finish(false)
  end
  if not storage.chart_tick then
    if game.tick - storage.start_tick < SETTLE then
      return
    end
    storage.chart_tick = game.tick
    force.chart(surface, { { -radius * 32, -radius * 32 }, { radius * 32, radius * 32 } })
    return
  end
  if (game.tick - storage.chart_tick) % 5 ~= 0 then
    return
  end
  if chunk_complete(force, surface) then
    export(force, surface)
    return finish(true)
  end
  if game.tick - storage.chart_tick > CAP then
    log("[foreman-chart] charting " .. planet .. " did not finish")
    return finish(false)
  end
end)
