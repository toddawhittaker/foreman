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
  `[CHAT] <server>: ...` lines. The RCON password is absent. Two things
  were replaced because the repository is public: the client's LAN address
  (now the documentation address 192.0.2.10) and the server padlock hash
  (now 32 X characters). Everything else, including the public user name,
  is as captured.

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
binary. Keeping `-i` (SPEC 4.3) still costs nothing and keeps a console open.

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
must watch the restart count (SPEC 8.7) or the `InitializationFailed` line.

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

## 10. Live operations (Epic 9 task 1)

Captured on 2026-11-12 with Docker 29.1.3 (API 1.52) from the same three
pinned images, on a fresh `--create` map per version. Each container ran
Foreman's own start script (`internal/game/container.go`, SPEC 4.3):
`--start-server-load-latest`, `--server-adminlist` and `--server-banlist`
in `config/`, `--server-settings`, and `--server-whitelist` only when
`config/server-whitelist.json` exists. RCON replies were read with a small
Go client, one connection per batch. The RCON password was random per run
and is absent from every file. Player names (alice, bob, ...) are made up;
no client was connected, so every player is offline and "not on map".

Files in each version folder:

- `rcon-players.log`: each RCON command and its reply (Go-quoted), with
  the contents and UTC mtime of the three list files after each step.
- `run-players.log`: the game's log for the same run (`docker logs -t`):
  `[BAN]`, `[UNBANNED]`, `[PROMOTE]`, `[DEMOTE]`, `[CHAT]`, `[COMMAND]`,
  `[WARNING]` lines.
- `whitelist-restart.log` with `run-whitelist-restart.log`,
  `run-whitelist-flag-missing.log` and `run-whitelist-use-flag.log`: the
  whitelist persistence runs. The lines `Error response from daemon: ...
  is not running` in it are the capture script listing files in a stopped
  container, not game output.
- `run-mod-lua-error.log`, `run-mod-missing-dep.log`, `mod-error-state.txt`:
  a mod whose `data.lua` calls `error(...)`, and a mod depending on a mod
  that is not installed; the state file is `docker inspect` after 20 s.
- `run-oom.log`, `oom-state.txt`: the start under `--memory 96m
  --memory-swap 96m`.

`docker/port-in-use.log` is Docker's own output and the same for every
image (taken with 2.1.24).

### Chat

A plain RCON command that does not start with `/` is said in chat by the
server, the same in all three versions:

```
2026-11-12 14:59:13 [CHAT] <server>: hello from rcon
```

The RCON reply is empty. **No client `[CHAT]` line was captured**: a
headless server cannot join another as a client. The only real-client
transcript is `2.1.24/join-leave.log`, which has no player chat.

### Player commands over RCON (offline players)

Identical text in all three versions (`rcon-players.log`):

| Command | Reply |
|---|---|
| `/kick alice griefing` (not on map) | `Player alice doesn't exist.\n` |
| `/kick` | `Player  doesn't exist.\n` (two spaces) |
| `/ban bob griefing` | `Added bob to the banlist.\n` |
| `/ban bob again` | `bob is already banned.\n` |
| `/ban carl` (no reason) | `Added carl to the banlist.\n` |
| `/ban` | `Invalid parameter\n<add/remove/get/clear> <player> - Adds or removes ...\n` |
| `/bans`, `/banlist get` | `Banned players: bob and carl\n`; empty: `The banlist is empty.\n` |
| `/unban bob` | `Unbanned bob.\n` |
| `/unban bob` again, `/unban` | `bob wasn't banned.\n`, ` wasn't banned.\n` |
| `/promote carol` | empty reply |
| `/promote carol` again | `carol is already in the admin list and will be promoted upon joining the game.\n` |
| `/demote carol` | empty reply |
| `/demote carol` again | `carol is not in the admin list.\n` |
| `/promote`, `/demote`, `/admins` | empty reply (`/admins` too, with carol in the admin list and nobody online) |
| `/whitelist add dave` | `Added dave to the whitelist.\n` |
| ... again | `dave is already whitelisted.\n` |
| `/whitelist get` | `Whitelisted players: dave\n`; two: `... dave and erin\n`; none: `The whitelist is empty.\n` |
| `/whitelist remove dave` | `Removed dave from the whitelist.\n`; again: `dave wasn't in the whitelist.\n` |
| `/whitelist enable`, `/whitelist disable` | empty reply, every time; no log line |
| `/whitelist clear` | `The whitelist has been cleared and disabled.\n`; again: `The whitelist is already empty.\n` |
| `/whitelist`, `/whitelist add` | `Invalid parameter\n<enable/disable/add/remove/get/clear> <player> - ...\n` |

Lists of three or more read `a, b, c and d`. The game log lines:

```
[BAN] bob (not on map) was banned by <server>. Reason: griefing.
[BAN] carl (not on map) was banned by <server>. Reason: unspecified.
[UNBANNED] bob was unbanned by <server>.
[PROMOTE] carol was added to the admin list by <server> and will be promoted upon joining the game.
[DEMOTE] carol was removed from the admin list by <server> and will no longer be promoted upon joining the game.
```

each prefixed `YYYY-MM-DD HH:MM:SS ` in server time. `[UNBANNED]` is
logged even when the player was not banned (`/unban nobody`, `/unban`),
and `[BAN]` even when already banned. Kicks and whitelist changes log
nothing.

Invalid names: **the game validates nothing**. A 70-character name is
banned, promoted and whitelisted. The player is the first word, so
`/ban "quoted name" x` bans `"quoted` with reason `name" x`. `/promote
ü-name` stores `ü-name` in 1.1.110 but **`u-name` in 2.0.77 and 2.1.24**
(the 2.x admin list folds the accent; the log line still says `ü-name`).
That is the one format difference between versions in these runs.

### Which list files the game writes, and when

With `--server-banlist` and `--server-adminlist`, the game writes the file
**at once**, on the command, in all three versions; it does not wait for
a save or exit. The files do not exist until the first change. Formats:

- `server-banlist.json`: a JSON array; an entry is `{"username", "reason"}`
  when a reason was given, a bare string when not. The file starts with an
  empty line and has blank lines before objects (see the log).
- `server-adminlist.json`: an array of strings; `[]` when emptied.
- `server-whitelist.json`: an array of strings, written at once on add,
  remove and clear, **but only when the game runs with
  `--use-server-whitelist --server-whitelist <file>`**.

Without `--server-whitelist`, `/whitelist add` writes no file anywhere (not
in `config/`, not in the write-data folder `/opt/factorio`, checked with
`docker diff`), and the list is lost when the game stops.

**Foreman's start script cannot pass `--server-whitelist` alone.** With
`config/server-whitelist.json` present, all three versions exit 1 at once:

```
Error CommandLineMultiplayer.cpp:NN: use-server-whitelist must be set when server-whitelist is set.
```

(`run-whitelist-flag-missing.log`). This contradicts SPEC 4.3 ("Foreman
never passes `--use-server-whitelist`") and is reported for a ruling.

### Ruling 8: does an in-game `/whitelist enable` survive a restart?

**No, in all three versions.** Evidence (`whitelist-restart.log`, run 2):
on Foreman's command, `/whitelist enable` then `/whitelist add dave` gave
`Whitelisted players: dave`; after `docker stop` and `docker start` of the
same container with the same save and config, `/whitelist get` replied
`The whitelist is empty.` No file holds the list or the enabled state, and
the save does not either. The enabled state itself cannot be read over
RCON (enable and disable reply empty and log nothing), so it was not seen
directly; a client join would show it. Whatever it is, the list it would
enforce is gone.

With `--use-server-whitelist --server-whitelist config/server-whitelist.json`
(run 4), the list survives: `/whitelist add hank`, `add ivy`, then
`/whitelist disable`, stop and start, and `/whitelist get` replied `hank
and ivy`. Under that flag, enforcement at start comes from the flag, not
from an in-game enable or disable (not observed directly without a
client). `/whitelist clear` wrote `[]`.

### A chat message that starts with `/`

**RCON strips leading spaces and runs the command**, all three versions:

- `" /c rcon.print(\"RAN-leading-1-space\")"` replied
  `RAN-leading-1-space` and logged `[COMMAND] <server> (command): ...`.
- `"   /c ..."` (three spaces), the first Lua command on the save, logged
  `[WARNING] Using Lua console commands will disable achievements. Please
  repeat the command to proceed.` So it was taken as a command.
- `"  /ban zed via-leading-space"` banned zed (`/bans` lists zed).
- `" /silent-command rcon.print(...)"` ran too.

So Foreman must refuse, not just prefix, a message whose first non-space
character is `/`.

### A mod that fails to load

The game exits 1 before loading the map (all three for the Lua error):

```
Error Util.cpp:NN: Failed to load mod "brokenlua": __brokenlua__/data.lua:1: deliberate failure for capture
stack traceback:
	[C]: in function 'error'
	__brokenlua__/data.lua:1: in main chunk
```

A missing dependency, 2.0.77 and 2.1.24 (exit 1; the bullets are U+2022):

```
Error Util.cpp:81: Failed to load mod "needsdep": 
• needsdep
    • Missing required dependency nonexistent-mod >= 1.0.0
```

**1.1.110 differs**: with the same missing dependency it logs nothing about
the mod and starts the game without it (`mod-error-state.txt`: running).
The message spans several lines, so a parser must take the lines after
`Failed to load mod`. The first line has a trailing space.

### Port already in use

`docker/port-in-use.log`. Starting a container whose published host port
is taken, from the CLI:

```
Error response from daemon: failed to set up container networking: driver failed programming external connectivity on endpoint <name> (<endpoint id>): Bind for 0.0.0.0:44199 failed: port is already allocated
```

From the Engine API (`POST /containers/<id>/start`): HTTP 500 with body
`{"message":"failed to set up container networking: ... Bind for 0.0.0.0:44198 failed: port is already allocated"}`,
and the container is left `created`, exit 128, with the same text in
`State.Error`. Binding `127.0.0.1:<port>` while another container holds
`0.0.0.0:<port>` fails the same way. **A second start of the same
container then succeeds (HTTP 204, running) with no port published**
(`docker port` empty). Foreman must not retry a start after this error.

### Out of memory

Under `--memory 96m --memory-swap 96m` all three are killed while loading
mods: `State.OOMKilled` true, exit 137, `State.Error` empty. The game log
just stops; it has no line about memory. Under `--restart unless-stopped`
(2.1.24, `oom-state.txt`) Docker restarts it (8 restarts in 30 s); while a
restarted attempt is running, `OOMKilled` reads false again, and it is true
again after the next kill and after `docker stop`.
