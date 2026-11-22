-- Foreman's save players reader (SPEC 8.14, 8.16, ADR 0030). The game runs
-- a copy of a save in benchmark mode with this mod added. The first time the
-- mod loads in the save, on_init writes script-output/saveplayers.json
-- (format 1): the game version, the tick, the mods the game loaded and every
-- player who has joined, with their play time in ticks. It adds no
-- prototypes and has no settings of its own. The same script runs on 1.1,
-- 2.0 and 2.1.
--
-- It reads neither the online flag nor the last-online date of a player:
-- both are wrong for a player who was online when the save was made, since
-- the save loads them as online now. A player's play time is not affected.

-- 1.1 has no helpers; its game, which exists only once events run, has
-- write_file and table_to_json.
local function writer()
  return helpers or game
end

-- whole turns a number into a whole number, since the output holds none
-- other.
local function whole(n)
  return math.floor(n or 0)
end

-- array_json writes a list of tables as a JSON array, one table_to_json call
-- for each element, because table_to_json writes an empty table as an object
-- and the reader wants a list.
local function array_json(h, items)
  local parts = {}
  for i, item in ipairs(items) do
    parts[i] = h.table_to_json(item)
  end
  return "[" .. table.concat(parts, ",") .. "]"
end

-- by_name orders mods and players the same way on every run.
local function by_name(a, b)
  return a.name < b.name
end

local function read()
  local h = writer()

  local mods = {}
  for name, version in pairs(script.active_mods) do
    mods[#mods + 1] = { name = name, version = version }
  end
  table.sort(mods, by_name)

  local players = {}
  for _, player in pairs(game.players) do
    players[#players + 1] = { name = player.name, online_ticks = whole(player.online_time) }
  end
  table.sort(players, by_name)

  -- The head is a table with fields that are never empty, so its closing
  -- brace is the last character and the two lists go in before it.
  local head = h.table_to_json({
    format = 1,
    game_version = script.active_mods["base"] or "",
    tick = whole(game.tick),
  })
  local json = string.sub(head, 1, -2)
    .. ',"active_mods":' .. array_json(h, mods)
    .. ',"players":' .. array_json(h, players) .. "}"
  h.write_file("saveplayers.json", json, false)
end

-- A mod added to a save runs on_init once. on_tick is the same read for a
-- game that does not, and the flag keeps either from writing twice.
local done = false

local function once()
  if not done then
    done = true
    read()
  end
end

script.on_init(once)
script.on_event(defines.events.on_tick, once)
