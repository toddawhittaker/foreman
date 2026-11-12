# Test data for mod settings

- `game-<version>.dat`: `mod-settings.dat` files written by the real game (factoriotools/factorio images 2.1.24, 2.0.77, 1.1.110) for the `fmtest` mod in `internal/game/testdata/modsettings`. Header versions differ per line; 2.1.24 has a final version byte of 1.
- `dump-data-1.1.110.json`: `script-output/mod-settings-dump.json` from `--dump-data` on 1.1.110, for reference (no owning mod field).
