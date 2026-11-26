-- Foreman's resource counter (SPEC 5.6). When the map is made, it generates
-- one planet's chunks for the picture's area, finds every resource entity in
-- it, one resource at a time, groups them into patches, then does the same
-- for the tiles of each fluid tied to a resource control, as spots, and
-- writes them as JSON to script-output/resources.json. It adds no
-- prototypes, so the map is the one
-- the preview draws. params.lua, written beside it for each run, returns
-- {radius = N, planet = "<name>"}: the picture's half width in chunks, and
-- the planet to count.
--
-- Ores join a patch when their distance on both axes is at most ore_gap (6)
-- tiles: below that, sparse ores break one island or field into many
-- crumbs, and on Nauvis no two separate fields join at any gap up to 16
-- (ADR 0015). A count with more than dense_ore_tiles ore tiles, far more
-- than any default map has, joins ores within dense_ore_gap (2) instead, so
-- its patches are finer: at 6 the richest maps ran into the time limit
-- before they ran out of memory, and the page said to try again where only
-- a smaller size helps (ADR 0015). Wells, the entities of a resource whose
-- product is a fluid, join when their distance on both axes is at most
-- well_gap (24) tiles; the real game's fields stop merging between 18 and
-- 40. The gaps and the threshold can be set in params.lua, which only the
-- real-game test does; a set ore_gap is used as it is. Fulgora's scrap joins
-- within 16 tiles (ADR 0015): at 2, high-frequency scrap gave each island
-- dozens of labels; a few labels can join islands across thin channels.
local params = require("params")
local radius, planet = params.radius, params.planet
local ore_gap = params.ore_gap or 6
local dense_ore_tiles = params.dense_ore_tiles or 400000
local dense_ore_gap = 2
local well_gap = params.well_gap or 24
local resource_gaps = { fulgora = { scrap = 16 } }
local planet_gaps = resource_gaps[planet] or {}

-- Gaps up to this are joined by per-tile offsets, larger ones by cells
-- (ADR 0015). Offsets look up every position in reach whether or not a tile
-- is there: 84 a tile at gap 6, 544 at 16. Cells compare only the tiles
-- present in the 3 x 3 cells around, about 160 a tile in a dense field at
-- 6, so they win only for sparse resources, which are the ones with large
-- gaps: scrap (16) and wells (24).
local offset_limit = 6

-- surface_of returns the planet's surface, making it on 2.x when the planet
-- has none yet. 1.1 has only Nauvis, which always has one.
local function surface_of(name)
  local s = game.surfaces[name]
  if s then
    return s
  end
  local p = game.planets[name]
  return p.surface or p.create_surface()
end

-- color_of turns a prototype's map colour into whole 0-255 values. On 1.1,
-- 2.0 and 2.1 alike the game reads a map colour back as r, g and b already
-- scaled to 0-255 (iron ore is 105, 133, 147), so it is only rounded.
local function color_of(c)
  if not c then
    return { 255, 255, 255 }
  end
  return { math.floor(c.r + 0.5), math.floor(c.g + 0.5), math.floor(c.b + 0.5) }
end

-- resource_prototypes returns every resource prototype: prototypes on 2.x,
-- the game's filtered list on 1.1.
local function resource_prototypes()
  local filter = { { filter = "type", type = "resource" } }
  if prototypes then
    return prototypes.get_entity_filtered(filter)
  end
  return game.get_filtered_entity_prototypes(filter)
end

-- is_well reports whether a resource's product is a fluid. Lithium brine is
-- a well that is not infinite.
local function is_well(proto)
  for _, product in pairs(proto.mineable_properties.products or {}) do
    if product.type == "fluid" then
      return true
    end
  end
  return false
end

-- controls_of returns the set of control names a tile's autoplace reaches:
-- its own control, and each control:<name>: in its expression and in the
-- named noise expressions and functions that expression uses, followed with
-- the planet's own names for them. Vesta's tiles name no control of their
-- own; their expression uses one that uses control:tritium:size.
local function controls_of(spec, renames)
  local found, seen = {}, {}
  if spec.control then
    found[spec.control] = true
  end
  local function walk(expr)
    if type(expr) ~= "string" then
      return
    end
    for c in expr:gmatch("control:([^:%s]+):") do
      found[c] = true
    end
    for id in expr:gmatch("[%a_][%w_]*") do
      local n = renames[id]
      if type(n) ~= "string" then
        n = id
      end
      if not seen[n] then
        seen[n] = true
        local named = prototypes.named_noise_expression[n] or prototypes.named_noise_function[n]
        if named then
          walk(named.expression)
        end
      end
    end
  end
  walk(spec.probability_expression)
  return found
end

-- fluid_tiles returns, by fluid, the tile prototypes that count as its spots,
-- the controls they are tied to, and a tile's map colour, the colour the
-- picture draws. A tile counts when it carries a fluid and every control its
-- autoplace reaches is a resource control, with at least one (SPEC 5.6). The
-- game's own oceans reach a terrain control or none, and its lava reaches
-- Vulcanus's volcanism as well as tungsten, so none of them count. 1.1 has
-- no fluid tiles and no prototypes global.
local function fluid_tiles(surface)
  local out = {}
  if not prototypes then
    return out
  end
  local renames = surface.map_gen_settings.property_expression_names or {}
  local names = {}
  for name in pairs(prototypes.tile) do
    names[#names + 1] = name
  end
  -- In name order, so a fluid's colour does not change from run to run.
  table.sort(names)
  for _, name in ipairs(names) do
    local t = prototypes.tile[name]
    local spec = t.fluid and t.autoplace_specification
    if spec then
      local controls, ok = {}, true
      for c in pairs(controls_of(spec, renames)) do
        local control = prototypes.autoplace_control[c]
        -- A name that is no control, as a word in an expression can be, is
        -- left out.
        if control then
          if control.category ~= "resource" then
            ok = false
          end
          controls[#controls + 1] = c
        end
      end
      if ok and #controls > 0 then
        local fluid = t.fluid.name
        local f = out[fluid]
        if not f then
          f = { tiles = {}, controls = {}, color = color_of(t.map_color), seen = {} }
          out[fluid] = f
        end
        f.tiles[#f.tiles + 1] = name
        for _, c in ipairs(controls) do
          if not f.seen[c] then
            f.seen[c] = true
            f.controls[#f.controls + 1] = c
          end
        end
      end
    end
  end
  for _, f in pairs(out) do
    table.sort(f.controls)
    f.seen = nil
  end
  return out
end

script.on_init(function()
  -- 1.1 has no helpers; its game, which exists only once events run, has
  -- write_file and table_to_json.
  local h = helpers or game
  local surface = surface_of(planet)
  surface.request_to_generate_chunks({ 0, 0 }, radius)
  surface.force_generate_chunk_requests()

  -- The ore tiles that would join at ore_gap, counted without keeping
  -- entity handles, decide whether this is a dense count.
  local half = radius * 32
  if not params.ore_gap then
    local names = {}
    for name, proto in pairs(resource_prototypes()) do
      if not is_well(proto) and not planet_gaps[name] then
        names[#names + 1] = name
      end
    end
    -- An empty name list would not filter at all.
    if #names > 0 then
      local ores = surface.count_entities_filtered({ area = { { -half, -half }, { half, half } }, name = names })
      if ores > dense_ore_tiles then
        ore_gap = dense_ore_gap
      end
    end
  end

  -- The picture covers tiles [-half, half) on each axis. A tile's key leaves
  -- room for neighbours up to the largest gap beyond each edge, and gives
  -- the tile back, so no coordinates are stored.
  local largest = math.max(ore_gap, well_gap)
  for _, g in pairs(planet_gaps) do
    largest = math.max(largest, g)
  end
  local pad = largest + 1
  local width = 2 * (half + pad)
  local function key(x, y)
    return (x + half + pad) * width + (y + half + pad)
  end
  local function tile(k)
    return math.floor(k / width) - half - pad, k % width - half - pad
  end

  -- Small gaps: each pair of tiles within the gap is looked at once, from
  -- the tile with the smaller (x, y).
  local function offsets_of(gap)
    local offsets = {}
    for dx = 0, gap do
      for dy = -gap, gap do
        if dx > 0 or dy > 0 then
          offsets[#offsets + 1] = { dx, dy }
        end
      end
    end
    return offsets
  end

  -- A set of tiles of one resource or fluid: n tiles, each tile's key, and
  -- each key's tile index.
  local function new_tiles()
    return { n = 0, keys = {}, at = {} }
  end

  -- add keeps a tile at x, y once, inside the picture, and returns its index,
  -- or nil when it is outside or kept already.
  local function add(t, x, y)
    if x < -half or x >= half or y < -half or y >= half then
      return nil
    end
    local k = key(x, y)
    if t.at[k] then
      return nil
    end
    t.n = t.n + 1
    t.keys[t.n], t.at[k] = k, t.n
    return t.n
  end

  -- group joins the tiles whose distance on both axes is at most gap and
  -- returns each patch's sums, box and label tile, in the order of its first
  -- tile. amounts, when given, is each tile's amount. It drops t.at once the
  -- tiles are joined, as the sums need only the keys.
  local function group(t, amounts, gap)
    local n, keys, at = t.n, t.keys, t.at
    -- Union-find over tile indices, with path halving.
    local parent = {}
    for i = 1, n do
      parent[i] = i
    end
    local function find(i)
      while parent[i] ~= i do
        parent[i] = parent[parent[i]]
        i = parent[i]
      end
      return i
    end
    local function union(a, b)
      a, b = find(a), find(b)
      if a < b then
        parent[b] = a
      elseif b < a then
        parent[a] = b
      end
    end

    if gap > offset_limit then
      -- Bucketed by gap-wide cells, so only the 3 x 3 cells around a tile
      -- can hold one within the gap.
      local cells = {}
      for i = 1, n do
        local x, y = tile(keys[i])
        local gx, gy = math.floor(x / gap), math.floor(y / gap)
        for ox = -1, 1 do
          for oy = -1, 1 do
            local list = cells[(gx + ox + 2048) * 4096 + gy + oy + 2048]
            if list then
              for _, j in ipairs(list) do
                local jx, jy = tile(keys[j])
                if math.abs(jx - x) <= gap and math.abs(jy - y) <= gap then
                  union(i, j)
                end
              end
            end
          end
        end
        local c = (gx + 2048) * 4096 + gy + 2048
        cells[c] = cells[c] or {}
        table.insert(cells[c], i)
      end
    else
      local offsets = offsets_of(gap)
      for i = 1, n do
        local x, y = tile(keys[i])
        for _, o in ipairs(offsets) do
          local j = at[key(x + o[1], y + o[2])]
          if j then
            union(i, j)
          end
        end
      end
    end
    at, t.at = nil, nil

    -- Each patch's sums, box and centroid, in the order of its first tile.
    local patch_of, sums = {}, {}
    for i = 1, n do
      local r = find(i)
      local x, y = tile(keys[i])
      local s = patch_of[r]
      if not s then
        s = { amount = 0, n = 0, sx = 0, sy = 0, x0 = x, y0 = y, x1 = x, y1 = y }
        patch_of[r] = s
        sums[#sums + 1] = s
      end
      if amounts then
        s.amount = s.amount + amounts[i]
      end
      s.n = s.n + 1
      s.sx, s.sy = s.sx + x, s.sy + y
      if x < s.x0 then s.x0 = x end
      if y < s.y0 then s.y0 = y end
      if x > s.x1 then s.x1 = x end
      if y > s.y1 then s.y1 = y end
    end
    -- The label tile is the patch's tile nearest its centroid; a centroid
    -- alone can fall outside a C-shaped patch.
    for i = 1, n do
      local s = patch_of[find(i)]
      local x, y = tile(keys[i])
      local dx, dy = x - s.sx / s.n, y - s.sy / s.n
      local d = dx * dx + dy * dy
      if not s.d or d < s.d then
        s.d, s.lx, s.ly = d, x, y
      end
    end
    return sums
  end

  local resources, patches = {}, {}
  -- One resource at a time keeps only its own tiles in memory: a key, an
  -- amount and a parent each.
  for name, proto in pairs(resource_prototypes()) do
    local t, amounts = new_tiles(), {}
    -- One chunk at a time keeps the number of entity handles small. A well
    -- near a chunk's edge is found from both chunks; its key keeps it once.
    for cx = -radius, radius - 1 do
      for cy = -radius, radius - 1 do
        local area = { { cx * 32, cy * 32 }, { cx * 32 + 32, cy * 32 + 32 } }
        for _, e in pairs(surface.find_entities_filtered({ area = area, name = name })) do
          local pos = e.position
          local i = add(t, math.floor(pos.x), math.floor(pos.y))
          if i then
            amounts[i] = e.amount
          end
        end
      end
    end

    if t.n > 0 then
      local gap = well_gap
      if not is_well(proto) then
        gap = planet_gaps[name] or ore_gap
      end
      local total = 0
      for _, s in ipairs(group(t, amounts, gap)) do
        total = total + s.amount
        patches[#patches + 1] = {
          name = name, x = s.lx, y = s.ly, amount = s.amount, n = s.n,
          box = { s.x0, s.y0, s.x1, s.y1 },
        }
      end
      local infinite = proto.infinite_resource and true or false
      resources[name] = {
        total = total, infinite = infinite,
        normal = infinite and proto.normal_resource_amount or 0,
        color = color_of(proto.map_color),
      }
    end
  end

  -- Then the spots of each fluid's tiles, one fluid at a time as above,
  -- joined by the ore gap (SPEC 5.6). A spot has a tile count and no amount,
  -- since pumping never runs it out.
  local fluids = {}
  for fluid, f in pairs(fluid_tiles(surface)) do
    local t = new_tiles()
    for cx = -radius, radius - 1 do
      for cy = -radius, radius - 1 do
        local area = { { cx * 32, cy * 32 }, { cx * 32 + 32, cy * 32 + 32 } }
        for _, found in pairs(surface.find_tiles_filtered({ area = area, name = f.tiles })) do
          local pos = found.position
          add(t, pos.x, pos.y)
        end
      end
    end
    if t.n > 0 then
      for _, s in ipairs(group(t, nil, ore_gap)) do
        patches[#patches + 1] = {
          name = fluid, fluid = true, x = s.lx, y = s.ly, n = s.n,
          box = { s.x0, s.y0, s.x1, s.y1 },
        }
      end
    end
    fluids[fluid] = { controls = f.controls, color = f.color }
  end
  -- table_to_json writes an empty table as [] or {}; the parser takes both.
  h.write_file("resources.json", h.table_to_json({ planet = planet, resources = resources, fluids = fluids, patches = patches }), false)
end)
