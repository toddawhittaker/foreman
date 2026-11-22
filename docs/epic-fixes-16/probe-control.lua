local OUT = "foreman-probe/out.jsonl"
local function line(t) helpers.write_file(OUT, helpers.table_to_json(t) .. "\n", true) end

local function dump(event_name)
  line({kind="event", event=event_name, tick=game.tick, ticks_played=game.ticks_played})
  for _, p in pairs(game.players) do
    line({kind="player", name=p.name, index=p.index, connected=p.connected,
          online_time=p.online_time, last_online=p.last_online})
  end
  local force = game.forces["player"]
  for _, s in pairs(game.surfaces) do
    local chunks, charted = 0, 0
    for c in s.get_chunks() do
      chunks = chunks + 1
      if force.is_chunk_charted(s, {c.x, c.y}) then charted = charted + 1 end
    end
    local by_type, n = {}, 0
    for _, e in pairs(s.find_entities_filtered{force = force}) do
      by_type[e.type] = (by_type[e.type] or 0) + 1
      n = n + 1
    end
    line({kind="surface", name=s.name, platform=(s.platform ~= nil), chunks=chunks,
          charted=charted, entities=n, by_type=by_type})
  end
  line({kind="done", event=event_name})
end

script.on_init(function()
  storage.dumped = true
  line({kind="fired", event="on_init", tick=game.tick})
  dump("on_init")
end)

script.on_configuration_changed(function(data)
  line({kind="fired", event="on_configuration_changed", tick=game.tick})
end)

script.on_event(defines.events.on_tick, function(e)
  line({kind="fired", event="on_tick", tick=e.tick})
  if not storage.dumped_tick then
    storage.dumped_tick = true
    dump("on_tick")
  end
end)
