# Golden fixtures for the map exchange decoder

SPEC 5.6: Foreman decodes a map exchange string itself. These files are the
proof: strings made by the real game, each with the game's own reading of it.

## What is here

For each of five game versions there is one folder, `1.1.110`, `2.0.66`,
`2.0.77`, `2.1.9` and `2.1.24`. In it, for each map:

- `<name>.txt` is the exchange string the game gives for that map.
- `<name>.json` is the game's own parse of that string: its
  `map_gen_settings` and `map_settings`, written by the game's
  `table_to_json`. Keys are sorted so a rerun gives the same bytes.

The maps, per version: one for each preset the game lists (`default`,
`rich-resources`, `marathon`, `death-world`, `death-world-marathon`,
`rail-world`, `ribbon-world`, `island`, and from 2.0 also `lakes`), made with a
fixed seed (1000 plus the preset's position), and two custom maps, `custom-1`
and `custom-2`. Custom maps change sizes, seed (one is above the signed 32-bit
limit), starting area, peaceful mode, some resource, water, tree and enemy
controls, cliffs, the terrain type, and in `custom-2` the map settings
(pollution, evolution, expansion, difficulty). On 2.x every map is made twice,
the second time with Space Age on, with `_space-age` added to the name. That
makes 98 maps in about 0.5 MB.

## How they were made

`generate.go` makes them. Run it from this folder, with Docker and the game
images already pulled:

    go run generate.go            # all five versions
    go run generate.go 2.1.24     # one version

For each map it runs a throwaway `factoriotools/factorio:<version>` container
with `--create`, passing `--preset` and `--map-gen-seed`, or
`--map-gen-settings` and `--map-settings`. The settings files are the game's
own example files from the same image, with the custom changes merged in. They
must come from the same image: `map-settings.json` has to hold every key the
version knows, and the keys differ between patches of one line. 2.1.9 needs
`max_pollution_to_restore_trees`, which the 2.1.24 example no longer has.

A small helper mod, `mxdump/`, is loaded with the map. Its `on_init` runs while
`--create` builds the map. It calls `game.get_map_exchange_string()`, then the
game's parse (`game.parse_map_exchange_string` on 1.1,
`helpers.parse_map_exchange_string` on 2.x), and writes both with
`write_file` into the game's `script-output` folder. No server, RCON or
save loading is needed, and each map takes about a second. On 2.x the mod list
turns the three Space Age mods (`elevated-rails`, `quality`, `space-age`) on
or off; `recycler` is always on in 2.1, as in a Foreman world (SPEC 9.7).

## How the golden test reads them

`../golden_test.go` runs `Decode` on every `.txt` and compares its map-gen and
map settings with the `.json` (a JSON compare, not a text compare). It also
checks that damaged strings are refused. `make test` runs it.

Where the game's JSON and the settings files say the same thing differently,
the test allows these and no other differences:

- The game reports an unlimited width or height as 2000000. The file says 0.
- The game writes every `property_expression_names` value as a string, even a
  number (`"1"`). The file may have a number.
- The game lists every autoplace control. A control that is all 1 (frequency,
  size, richness) may be left out of the decoded side, and a missing value
  inside a control means 1.
- The game writes an empty table as `[]`. The file has `{}`.
- Whole numbers must match exactly. Other numbers match within float32
  rounding (`0.17` is `0.17000000178813934` in the game).
- `default_enable_all_autoplace_controls` is ignored when true, the game's
  default; the 1.1 parse leaves it out.
- Fields the files format does not set, which the game fills in:
  `default_enable_all_autoplace_controls`, `no_enemies_mode`,
  `starting_points`, `autoplace_settings`, `cliff_settings.name`,
  `cliff_settings.control` and `cliff_settings.cliff_smoothing`. They are
  ignored when the decoder leaves them out, and must be right when it does not.

The game itself refuses a string without its `>>>` and `<<<` markers ("Wrong
quote characters", checked on 1.1.110 and 2.1.24) and accepts spaces and
newlines around one. The decoder accepts spaces and newlines the same way. It
takes a string with both markers, or with neither (pasted inner text), and
refuses one with exactly one marker, which is a cut-off paste.

Map settings have no allowed differences: the string holds every value.

## Map previews

Measured on this machine, 2026-11-06, with `--generate-map-preview` at 1024
pixels, the `default` preset and seed 1234, in a throwaway container as the
host user. "Seconds" is the whole `docker run`, including container start and
mod loading; the game's own preview step took 0.06 to 0.44 s.

| Version | Space Age | Planet | Seconds | PNG bytes |
|---|---|---|---|---|
| 1.1.110 | no Space Age on 1.1 | nauvis | 0.9 | 213,265 |
| 2.0.77 | off | nauvis | 1.5 | 393,886 |
| 2.1.24 | off | nauvis | 1.7 | 394,074 |
| 2.0.77 | on | nauvis | 2.5 | 393,886 |
| 2.0.77 | on | vulcanus | 2.4 | 528,593 |
| 2.0.77 | on | gleba | 2.4 | 542,027 |
| 2.0.77 | on | fulgora | 2.7 | 286,667 |
| 2.0.77 | on | aquilo | 2.3 | 164,284 |
| 2.1.24 | on | nauvis | 2.7 | 394,074 |
| 2.1.24 | on | vulcanus | 2.7 | 528,593 |
| 2.1.24 | on | gleba | 2.7 | 542,027 |
| 2.1.24 | on | fulgora | 3.0 | 287,852 |
| 2.1.24 | on | aquilo | 2.6 | 164,284 |

With Space Age off, `--map-preview-planet vulcanus` exits 1 with
`Invalid map-preview-planet: 'vulcanus'`, so a preview must ask only for the
planets the world has (`mapgen.Planets`). Space Age loads more mods, which is
why those runs are slower. A world's preview costs one run per planet.
