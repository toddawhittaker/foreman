#!/bin/bash
# Records the game's own map exchange string for each test save, using
# LuaSurface.get_map_exchange_string() from a probe mod. Usage: mxgen.sh OUTDIR
# Throwaway containers, no network, a copy of each save; never RCON.
set -eu
OUT=$1
HERE=$(cd "$(dirname "$0")" && pwd)
SAVES=$(cd "$HERE/.." && pwd)
mkdir -p "$OUT"

IMG11=factoriotools/factorio:1.1.110
IMG20=factoriotools/factorio:2.0.77
IMG21=factoriotools/factorio:2.1.24

# run SAVE IMAGE PROBE MODLIST(dir in $HERE holding mod-list.json and mods, or "")
run() {
  local save=$1 img=$2 probe=$3 mods=$4
  local work
  work=$(mktemp -d)
  mkdir -p "$work/mods" "$work/data" "$work/save"
  cp "$SAVES/$save" "$work/save/"
  cp -r "$HERE/$probe/." "$work/mods/"
  if [ -n "$mods" ]; then cp -r "$HERE/$mods/." "$work/mods/"; fi
  python3 -I - "$work/mods/mod-list.json" <<'PY'
import json, os, sys
p = sys.argv[1]
d = json.load(open(p)) if os.path.exists(p) else {"mods": [{"name": "base", "enabled": True}]}
d["mods"].append({"name": "foreman-mx-probe", "enabled": True})
json.dump(d, open(p, "w"))
PY
  chmod -R a+rwX "$work"
  docker run --rm --name "foreman-x3-mx-${save%.zip}" --network none \
    --memory 4g --stop-timeout 5 --entrypoint sh \
    -v "$work:/w" "$img" -c \
    "timeout 300 /opt/factorio/bin/x64/factorio --benchmark /w/save/$save --benchmark-ticks 1 --mod-directory /w/mods >/w/log.txt 2>&1; cp /factorio/script-output/mx.txt /w/mx.txt 2>/dev/null || cp /opt/factorio/script-output/mx.txt /w/mx.txt"
  tr -d '\n' < "$work/mx.txt" > "$OUT/${save%.zip}.txt"
  rm -rf "$work"
}

run 1.1.110-vanilla.zip "$IMG11" mxprobe-1.1 ""
run 1.1.110-testmod.zip "$IMG11" mxprobe-1.1 mods11
run 2.0.77-space-age.zip "$IMG20" mxprobe-2.0 ""
run 2.0.77-no-space-age.zip "$IMG20" mxprobe-2.0 mods20-nosa
run 2.1.24-space-age.zip "$IMG21" mxprobe-2.1 ""
run 2.1.24-no-space-age.zip "$IMG21" mxprobe-2.1 mods20-nosa
run 2.1.24-space-age-testmod.zip "$IMG21" mxprobe-2.1 mods21-sa-mod
