-- Foreman's resource counter (SPEC 5.6). When the map is made, it generates
-- one planet's chunks for the picture's area, finds every resource entity in
-- it, one resource at a time, groups them into patches and writes them as
-- JSON to script-output/resources.json. It adds no prototypes, so the map is the one
-- the preview draws. params.lua, written beside it for each run, returns
-- {radius = N, planet = "<name>"}: the picture's half width in chunks, and
-- the planet to count.
--
-- Ores join a patch when their distance on both axes is at most ore_gap (2)
-- tiles: touching, corners included (YARM's rule), or one tile apart, since
-- sparse ores such as Fulgora's scrap otherwise break into thousands of
-- fragments. Wells, the entities of a resource whose product is a fluid,
-- join when their distance on both axes is at most well_gap (24) tiles; the
-- real game's fields stop merging between 18 and 40. Both gaps can be set in
-- params.lua, which only the real-game test does.
local params = require("params")
local radius, planet = params.radius, params.planet
local ore_gap = params.ore_gap or 2
local well_gap = params.well_gap or 24

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

script.on_init(function()
  -- 1.1 has no helpers; its game, which exists only once events run, has
  -- write_file and table_to_json.
  local h = helpers or game
  local surface = surface_of(planet)
  surface.request_to_generate_chunks({ 0, 0 }, radius)
  surface.force_generate_chunk_requests()

  -- The picture covers tiles [-half, half) on each axis. A tile's key leaves
  -- room for neighbours up to the largest gap beyond each edge, and gives
  -- the tile back, so no coordinates are stored.
  local half = radius * 32
  local pad = math.max(ore_gap, well_gap) + 1
  local width = 2 * (half + pad)
  local function key(x, y)
    return (x + half + pad) * width + (y + half + pad)
  end
  local function tile(k)
    return math.floor(k / width) - half - pad, k % width - half - pad
  end

  -- Ores: each pair of tiles within ore_gap is looked at once, from the
  -- tile with the smaller (x, y).
  local offsets = {}
  for dx = 0, ore_gap do
    for dy = -ore_gap, ore_gap do
      if dx > 0 or dy > 0 then
        offsets[#offsets + 1] = { dx, dy }
      end
    end
  end

  local resources, patches = {}, {}
  -- One resource at a time keeps only its own tiles in memory: a key, an
  -- amount and a parent each.
  for name, proto in pairs(resource_prototypes()) do
    local at, keys, amounts = {}, {}, {}
    local n = 0
    -- One chunk at a time keeps the number of entity handles small. A well
    -- near a chunk's edge is found from both chunks; its key keeps it once.
    for cx = -radius, radius - 1 do
      for cy = -radius, radius - 1 do
        local area = { { cx * 32, cy * 32 }, { cx * 32 + 32, cy * 32 + 32 } }
        for _, e in pairs(surface.find_entities_filtered({ area = area, name = name })) do
          local pos = e.position
          local x, y = math.floor(pos.x), math.floor(pos.y)
          if x >= -half and x < half and y >= -half and y < half then
            local k = key(x, y)
            if not at[k] then
              n = n + 1
              keys[n], amounts[n] = k, e.amount
              at[k] = n
            end
          end
        end
      end
    end

    if n > 0 then
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

      if is_well(proto) then
        -- Wells: bucketed by well_gap-wide cells, so only the 3 x 3 cells
        -- around a well can hold one within the gap.
        local cells = {}
        for i = 1, n do
          local x, y = tile(keys[i])
          local gx, gy = math.floor(x / well_gap), math.floor(y / well_gap)
          for ox = -1, 1 do
            for oy = -1, 1 do
              local list = cells[(gx + ox + 2048) * 4096 + gy + oy + 2048]
              if list then
                for _, j in ipairs(list) do
                  local jx, jy = tile(keys[j])
                  if math.abs(jx - x) <= well_gap and math.abs(jy - y) <= well_gap then
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
      at = nil

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
        s.amount = s.amount + amounts[i]
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

      local total = 0
      for _, s in ipairs(sums) do
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
  -- table_to_json writes an empty table as [] or {}; the parser takes both.
  h.write_file("resources.json", h.table_to_json({ planet = planet, resources = resources, patches = patches }), false)
end)
