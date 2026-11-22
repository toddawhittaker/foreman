# Feasibility: benchmark-mode helper mod (#769, #773) and `/time` over RCON

Date 2026-11-21. Images by digest from the Makefile, lines 13-14:
2.0.77 `sha256:312341f2...` and 2.1.24 `sha256:a0c019b7...`. Scripts: `run.sh` (benchmark),
`server.sh` (throwaway server), `tools/rcon.py`, helper mod in `helper/foreman-probe/`.
Every run used a copy of the save in `runs/<tag>/`. No volumes were created (bind mounts only), and
every container was removed (`--rm`, or `docker rm -f` for the server).

## Saves used (copies)

| Copy | Source | Size | Game version of save | Mods |
|---|---|---|---|---|
| small-2.0.zip | internal/savefile/testdata/2.0.77-space-age.zip | 173,570 B | 2.0.77 | Space Age only |
| small-2.1.zip | internal/savefile/testdata/2.1.24-space-age.zip | 171,145 B | 2.1.24 | Space Age only |
| cur-2.1.zip | trial world `current`, _autosave22.zip | 32,137,505 B | 2.1.21 | Space Age + 2 mods |
| dans-2.0.zip | trial world `dans-achievement-saves`, _autosave10.zip | 93,572,133 B | 2.0.76 | Space Age only |
| big-2.0.zip | trial world `saves-2026`, _autosave27.zip | 186,515,194 B | 2.0.66 | Space Age + 44 mod files (159 MB) |

## Command form (entrypoint overridden)

```
docker run --rm --user $(id -u):$(id -g) --network none --entrypoint sh -v $R:/work IMAGE -c \
  '/opt/factorio/bin/x64/factorio -c /work/config.ini --mod-directory /work/mods \
   --benchmark /work/save.zip --benchmark-ticks 8 --benchmark-runs 1; cat /sys/fs/cgroup/memory.peak'
```
`config.ini` is two lines that set `write-data=/work/wd`, so `script-output/`, the log and `temp/` land
in the run folder. No game port and no network are needed (`--network none` worked).

## 1. Does benchmark mode run? Yes, on both versions.

Small saves, no helper: exit 0, "Performed 8 updates in 3.260 ms" (2.0.77). Same on 2.1.24.

## 2. Does the helper mod's script run and write script-output? Yes, on both.

The helper writes JSON lines with `helpers.write_file(..., append=true)` and `helpers.table_to_json`.
**All three events fire** in benchmark mode on an existing save that lacked the mod, in this order:
`on_init` (Factorio runs on_init for a mod newly added to a save), then `on_configuration_changed`, then
`on_tick` 8 times. Use `on_init` for a one-shot dump; `on_tick` with a `storage` guard also works.

Real 2.1 save (cur-2.1, 2.1.24), excerpt of out.jsonl:
```
{"kind":"fired","event":"on_init","tick":8352001}
{"kind":"event","event":"on_init","tick":8352001,"ticks_played":8352001}
{"kind":"player","name":"toddawhittaker","index":1,"connected":true,"online_time":8282296,"last_online":8352000}
{"kind":"player","name":"OrcScorpion","index":2,"connected":false,"online_time":3493613,"last_online":5704435}
{"kind":"surface","name":"nauvis","platform":false,"chunks":9519,"charted":4881,"entities":89275,"by_type":{"transport-belt":43582,...}}
{"kind":"fired","event":"on_configuration_changed","tick":8352001}
{"kind":"fired","event":"on_tick","tick":8352001} ... through 8352008
```
Surfaces found: nauvis, vulcanus, fulgora, platform-1..5 (chunks / charted / player-force entities, e.g.
fulgora 3181 / 1135 / 11509). Charted counts used `force.is_chunk_charted(surface, chunk)` over
`surface.get_chunks()`.

Big 2.0 save (2.0.77): four players including three offline, every surface including 27 platforms
and the EditorExtensions test surface (446,341 chunks, 722 charted):
```
{"kind":"event","event":"on_init","tick":127325755,"ticks_played":127332003}
{"kind":"player","name":"toddawhittaker","online_time":126975952,"last_online":127332002,"connected":true}
{"kind":"player","name":"OrcScorpion","online_time":3747451,"last_online":8251362,"connected":false}
{"kind":"player","name":"Jayjayxd","online_time":1711946,"last_online":3183189,"connected":false}
{"kind":"player","name":"Brewt","online_time":696285,"last_online":5816600,"connected":false}
nauvis 41976 chunks / 34326 charted / 462949 entities; vulcanus 245539; fulgora 76864; gleba 55465; aquilo 55550
```

Gotchas found:
- **info.json `factorio_version` must match the major version.** "2.0" is refused by 2.1.24:
  `Failed to load mod "foreman-probe": Incompatible Factorio version (current: 2.1, required: 2.0)`,
  exit 1. So the helper needs one info.json per game series (written at run time is enough).
- **`connected` is not trustworthy in benchmark mode.** Players who were online when the save was made
  load as connected, and their `last_online` reads as the current tick (dans-2.0: both players
  `connected:true`, `last_online` 59760004 = tick - 1). `online_time` is unaffected.
- **`game.tick` and `game.ticks_played` can differ** (big save: 127,325,755 vs 127,332,003, about
  1.7 minutes). Decide which one "total play time" means; ticks_played is the one that is not
  changed by mods that set game.tick.
- Entity counts differ slightly between on_init and on_tick (cargo pods and similar change as other
  mods' on_configuration_changed and the first tick run). Immaterial for a picture.

## 3. Mod list, Space Age, and does benchmark write the save?

- **The helper does not need to be in mod-list.json.** With it in the folder but not in the list
  (run `cur21-nolist`), Factorio enabled it and **wrote it into the copy's mod-list.json**.
- **Factorio also rewrites `mod-settings.dat`** (big run: bytes differ from byte 5, the version
  header). So the mod folder (at least mod-list.json and mod-settings.dat) must be a copy, never the
  world's own folder.
- **Space Age saves load** with their DLC mods plus the world's mods plus the helper: cur-2.1 (2 mods)
  and big-2.0 (44 mod files, saved on 2.0.66, run on 2.0.77) both exit 0.
- **Missing mods do NOT fail the run.** cur-2.1 with an empty mod folder still loaded and exited 0,
  with the mods silently dropped. Fail safe therefore needs Foreman to check the mod set itself
  (or have the helper write `script.active_mods` and compare with the save's mod list).
- A newer save on an older game fails cleanly: small-2.1 on 2.0.77 gives
  `Map version 2.1.24-1 cannot be loaded because it is higher than the game version (2.0.77-0)`, exit 1.
- **Benchmark mode does not write the save.** sha256 of every copy was identical before and after:
  - small-2.0 `afb9290c6a9db027cf74b49d816c84f85a8b916d1700c3c7e27e6bc01d1ee750` (both runs)
  - small-2.1 `394a6466eebd7996d774a8bf7e33c196ab2c80a9460fb0b4d7d0ff5d4bd1aa22`
  - cur-2.1 `216ac2622797b61124deecb0a6252702153821bd46bb11f91fda0c32fc3b9dae` (3 runs)
  - dans-2.0 `d1c644d778bfa2b740f2e3dc9b2386022f1358158768ff3f107db084186142bf`
  - big-2.0 `3b46e8124874ef32786c8ca3b2ca4d0d29f8ec9c062f828ed4a120a014555815`
- **Originals untouched:** `orig-sha/before.txt` and `orig-sha/after.txt` (sha256 of the three trial
  originals, taken before copying and at the end) are identical.
- Files benchmark mode writes in write-data: `factorio-current.log`, `player-data.json`, `temp/`,
  `script-output/`. Nothing in the saves folder.

## 4. Time and peak memory (cgroup memory.peak; wall time includes container start)

| Run | Version | Save | Wall time | Peak memory |
|---|---|---|---|---|
| small20-none | 2.0.77 | 174 KB, no helper | 1.0 s | 336 MB |
| small20-helper | 2.0.77 | 174 KB | 1.0 s | 353 MB |
| small21-helper | 2.1.24 | 171 KB | 1.1 s | 423 MB |
| cur21-add | 2.1.24 | 32 MB, 2 mods | 5.0 s | 1.50 GB |
| dans20-add | 2.0.77 | 94 MB, no mods | 12.3 s | 2.83 GB |
| big20-add | 2.0.77 | 187 MB, 44 mods | 43.6 s | 6.87 GB |

On the big save, mod loading took about 27 s of the 43 s (hd_planets alone is a 124 MB mod); the 8
ticks took 3.3 s, most of it the helper's first-tick dump.

## 5. `/time` over RCON on a throwaway server

Server: `--start-server /work/save.zip --port 34990 --rcon-port 27015`, RCON published on
`127.0.0.1:<random>` (32908 and 32909), random password, game port not published. Note: the
server contacts auth.factorio.com for a server padlock on start.

| Version | Save | `/time` reply | Same value from Lua |
|---|---|---|---|
| 2.0.77 | dans-2.0 | `276 hours and 40 minutes` | tick 59,760,005 = 276.67 h |
| 2.1.24 | cur-2.1 | `38 hours and 40 minutes` | `/c rcon.print(game.tick)` 8,352,005 = 38.67 h |

- `/time` reports **total game play time** (`/help` text: "Prints info about how old the map is"),
  as English text "H hours and M minutes", resolution one minute, not ticks. A paused server
  (autopause, nobody online) did not advance it between two calls 3 s apart.
- **Achievements:** `/time` is a plain server command and returned its answer at once. The first
  `/c` over RCON returned an empty reply and did not run; the second `/c` ran. That is the game's
  "this will disable achievements, repeat to confirm" gate, which `/time` does not trigger. There is
  no log line either way, so this is behavioural evidence rather than a flag read from the save.
- The server's save copies hashed the same afterwards (server never saved).
