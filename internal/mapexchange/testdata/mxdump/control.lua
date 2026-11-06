-- Runs once while the new map is created: writes the map's exchange string and
-- the game's own parse of it. 1.1 has these calls on game, 2.x on helpers.
script.on_init(function()
  local api = rawget(_G, "helpers") or game
  local s = game.get_map_exchange_string()
  api.write_file("exchange.txt", s, false)
  api.write_file("parsed.json", api.table_to_json(api.parse_map_exchange_string(s)), false)
end)
