# Real-game transcripts

Raw output from the three pinned `factoriotools/factorio` images, captured
on 2026-11-03 with Docker 29.9.2. The log parser and the fake game are
built from these files, so they are real output and must not be edited by
hand. The RCON password was random per run and was checked to be absent
from every file. The game itself prints it as `<private>`.

| Folder | Image (tag@digest, as in the Makefile) | Size |
|---|---|---|
| `1.1.110/` | `factoriotools/factorio:1.1.110@sha256:6fb9b7bfb12fb51bf668fce7e29cb1e98053f248201e64dfee870ef73b2f31cb` | 473 MB |
| `2.0.77/` | `factoriotools/factorio:2.0.77@sha256:312341f29987b5a6b11f00ba8240192f7564936af9913384d13f7f4c535708ef` | 556 MB |
| `2.1.24/` | `factoriotools/factorio:2.1.24@sha256:a0c019b7608975a58f1694de54a2326e13a5aa2d123c18f48e5396181e366e28` | 582 MB |

Sizes are the unpacked size Docker reports. 1.1.110 is the last 1.1
release; 2.0.77 the newest 2.0.x on Docker Hub.

## Files in each folder

- `create.log`: `factorio --create` making the small test map.
- `run-sigterm.log`: start, three RCON connections, `/server-save`, one
  autosave, then `docker stop` (SIGTERM). Timestamps from `docker logs -t`.
- `run-sigint.log`: start, then `docker kill -s INT`.
- `run-sigkill.log`: start, then `docker kill` (SIGKILL).
- `run-no-stdin.log`: started without `-i`.
- `run-corrupt.log`: 4 KB of random bytes as the save, under
  `unless-stopped`, 30 seconds of the crash loop.
- `run-truncated.log`: the real save cut to 200,000 bytes, same.
- `run-unreadable.log`: the real save with mode 000, same.
- `rcon.log`: raw RCON replies (item 9).
- `run-summary.log`: container state, timings and RCON replies per step.
- `mod-list.json`: what the game wrote into an empty `mods/`.
- `2.1.24/join-leave.log` (that folder only): the game's output from
  Foreman's own real-game check on 2026-11-03, with a real Factorio 2.1.24
  client joining, leaving and rejoining, and a stop with a 30-second
  countdown. It was started by Foreman, not by the spike, so it has no
  Docker timestamps. The player lines are `YYYY-MM-DD HH:MM:SS [JOIN]
  <name> joined the game` and `... [LEAVE] <name> left the game`, in
  server local time (UTC in the container). The stop warnings appear as
  `[CHAT] <server>: ...` lines. The RCON password is absent; the log holds
  a private network address and a public user name, kept as captured.

## 1. Command line

Same for all three. No entrypoint, our own uid and gid, one bind mount:

```
docker run -d -i --restart unless-stopped \
  --entrypoint /bin/sh -u <uid>:<gid> \
  --cap-drop ALL --security-opt no-new-privileges \
  --log-driver json-file --log-opt max-size=10m --log-opt max-file=3 \
  -v <host data>/worlds/<id>:/factorio \
  factoriotools/factorio:<tag>@<digest> \
  -c 'exec /opt/factorio/bin/x64/factorio --start-server "/factorio/saves/$1" \
      --server-settings /factorio/config/server-settings.json \
      --mod-directory /factorio/mods --rcon-port 27015 \
      --rcon-password "$(cat /factorio/config/rcon-password)"' \
  sh <save>.zip
```

The world folder holds `saves/`, `mods/` and `config/` (with
`rcon-password`, mode 0640, and `server-settings.json`). The password is
read inside the container, so it is not in the container's env or command
(checked with `docker inspect`). It is in the game process's arguments
inside the container, where the game logs it as `<private>`.

Write-data: no `config.ini` is needed in the world folder. Without `-c`
the game uses the image's `/opt/factorio/config/config.ini`, whose
write-data is `/opt/factorio`. That folder is mode 0777 in all three
images, so any uid can write there. It lives in the container's own layer,
so `player-data.json`, `factorio-current.log` and the like are lost when
the container is recreated. Saves and `mod-list.json` go to the bind
mount, because of `--start-server` and `--mod-directory`.

`--server-settings` is optional; without it the game starts with its
defaults. The spike used it to set `autosave_interval` to 1 minute and
`auto_pause` false, so an autosave happens with nobody on.

`mod-list.json` was written with mode 0646 (world-writable) by all three.

## 2. Stdin and TTY

No TTY is needed. With stdin open (`-i`) the server runs. Without `-i`,
stdin is at EOF from the start: all three log one line and keep running.
They did not exit (container still running after 15 seconds):

- 1.1.110: `Error InterruptibleStdioStream.cpp:61: Got EOF on stdin; closing`
- 2.0.77: `Error InterruptibleStdioStream.cpp:55: Got EOF on stdin; closing`
- 2.1.24: `Error InterruptibleStdioStream.cpp:72: Got EOF on stdin; closing`

So EOF closes the console reader only, not the server. The Epic 2 note
that the server exits on EOF came from the image's entrypoint, not the
binary. Keeping `-i` (R14) still costs nothing and keeps a console open.

## 3. Stop signal and timeout

SIGTERM and SIGINT both save cleanly in all three versions. Each logs
`Received SIGTERM, shutting down` (or `SIGINT`), `Quitting: signal.`,
`Saving map as /factorio/saves/<save>.zip`, `Saving progress` lines, the
multiplayer shutdown, and `Goodbye`, then exits with code 0. The save is
written to the loaded file name, not an autosave slot.

`docker stop` (SIGTERM) on the small test map (about 0.5 to 0.8 MB) took
0.44 to 0.66 seconds end to end; SIGINT took 1.1 to 1.2 seconds measured
by polling. Docker's default stop signal (SIGTERM) works, so no
`StopSignal` needs to be set. Save time grows with the map; the timeout
should be generous (tens of seconds) rather than tuned to this map. A
large real map was not measured.

## 4. Restart policy after kill and stop

Under `unless-stopped`, all three:

- `docker stop`: stays stopped, exit 0, restart count 0.
- `docker kill -s INT`: stays stopped, exit 0, restart count 0.
- `docker kill` (SIGKILL): stays stopped, exit 137, restart count 0.

So Docker treats a kill through its API as a manual stop, like stop.

## 5. Log lines

The first column is seconds since start. The source line numbers differ by
version, so a parser should match the text, not `File.cpp:NNN`.

Loading a save (all three): `Loading map /factorio/saves/spike.zip: <n> bytes.`
then `Loading level.dat: <n> bytes.` and `Info Scenario.cpp:NNN: Map version <x.y.z-n>`.

Reaching InGame:

- 1.1.110: `Info ServerMultiplayerManager.cpp:814: updateTick(0) changing state from(CreatingGame) to(InGame)`
  (the tick is the save's tick; it is not always 0)
- 2.0.77 and 2.1.24: the same text from `ServerMultiplayerManager.cpp:808` / `:809`.

RCON ready (after InGame in all three):
`Info RemoteCommandProcessor.cpp:NNN: Starting RCON interface at IP ADDR:({0.0.0.0:27015})`

`/server-save` completing:

```
Info AppManager.cpp:NNN: Saving game as /factorio/saves/spike.zip
Info AppManagerStates.cpp:NNN: Saving finished
```

Autosave completing:

```
Info AppManager.cpp:NNN: Saving to _autosave1 (blocking).
Info AppManagerStates.cpp:NNN: Saving finished
```

With `non_blocking_saving` true the first line would say otherwise; the
spike used false.

Player join and leave: the spike had no game client, so the other
transcripts hold no `[JOIN]` or `[LEAVE]` lines. They were captured later
with a real 2.1.24 client and are in `2.1.24/join-leave.log`:

```
2026-11-04 03:53:41 [JOIN] toddawhittaker joined the game
2026-11-04 03:54:01 [LEAVE] toddawhittaker left the game
```

Crash on a bad save (corrupt or truncated zip, or a file the game cannot
read), all three:

```
Loading map /factorio/saves/corrupt.zip: 4096 bytes.
Error ServerMultiplayerManager.cpp:NNN: MultiplayerManager failed: "Opening zip /factorio/saves/corrupt.zip failed: <reason>"
Info ServerMultiplayerManager.cpp:NNN: updateTick(<max>) changing state from(CreatingGame) to(InitializationFailed)
...
Goodbye
```

The reason is `Bad zip file` for 1.1.110 with a corrupt or truncated zip
and `I/O error` for an unreadable file. 2.0.77 and 2.1.24 say `I/O error`
for all three cases.

Crash loop under `unless-stopped`: yes, all three. After 30 seconds each
had restarted 7 or 8 times (Docker's backoff doubles from 100 ms). The
exit code differs: 2.0.77 and 2.1.24 exit 1, but **1.1.110 exits 0 after
a failed load**. A crash detector cannot rely on a non-zero exit code; it
must watch the restart count (R13) or the `InitializationFailed` line.

## 6. RCON replies

Same in all three, with nobody on:

- `/players online`: `Online players (0):\n`
- `/players`: `Players (0):\n`
- `/server-save`: `Saving the map\n` (sent at once; the save finishes
  later, shown by `Saving finished` in the log)

Players present were not observed (no client).

## 7. Built-in mods in mod-list.json

Written by the game into an empty `mods/`, all enabled:

- 1.1.110: `base`
- 2.0.77: `base`, `elevated-rails`, `quality`, `space-age`
- 2.1.24: `base`, `elevated-rails`, `quality`, `recycler`, `space-age`

2.1 adds `recycler` as a separate built-in mod.

## 8. Digests and sizes

See the table at the top. The digest is the multi-platform index digest
from Docker Hub, the same value Docker shows as the image id. A local
`2.1.24` tag pulled earlier had a different digest (`sha256:0ef39475...`);
the tag was rebuilt upstream, which is why it must be pinned by digest.

## 9. RCON empty command and long replies

From `rcon.log`. A command (id 10) and a second packet (id 11) were sent
back to back on one connection.

- **An empty command gets no reply at all**, in all three versions. Not
  after a real command, and not on its own. Nothing arrived within 5
  seconds. A client that waits for the empty-command reply to end a
  multi-packet answer will hang.
- Every non-empty command gets exactly one reply packet (type 0, id equal
  to the request id), even when its output is empty: the first `/c` on a
  fresh save replies with an empty body (2.0.77 and 2.1.24; the 1.1.110
  save had already run one).
- Replies are not split. `/help` is one 130-byte packet. A `/c` printing
  63,000 bytes arrived as one packet with length field 63,010, far above
  the 4,096-byte limit of the Source RCON spec. A client must read the
  whole length the header gives, and must accept large packets.
- Two real commands sent back to back are answered in order (id 10, then
  id 11).
