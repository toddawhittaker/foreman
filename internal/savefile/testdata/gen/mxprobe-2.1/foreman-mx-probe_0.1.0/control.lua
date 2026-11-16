-- Writes the game's own map exchange string for the loaded save.
local function dump()
  local s = game.surfaces[1].get_map_exchange_string()
  if helpers then helpers.write_file("mx.txt", s) else game.write_file("mx.txt", s) end
end
script.on_init(dump)
