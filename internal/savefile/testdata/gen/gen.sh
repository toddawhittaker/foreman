#!/bin/bash
# Generates fixture saves. Usage: gen.sh OUTDIR
set -eu
OUT=$1
HERE=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$OUT"
U=$(id -u); G=$(id -g)

IMG11=factoriotools/factorio:1.1.110@sha256:6fb9b7bfb12fb51bf668fce7e29cb1e98053f248201e64dfee870ef73b2f31cb
IMG20=factoriotools/factorio:2.0.77@sha256:312341f29987b5a6b11f00ba8240192f7564936af9913384d13f7f4c535708ef
IMG21=factoriotools/factorio:2.1.24@sha256:0ef394757b906f8e2e42d7e1f05332ad7cf7575c40d9b2412fa4c477189bdea7

# run NAME IMAGE MODDIR(in $HERE, or "") SAVE
run() {
  local name=$1 img=$2 mods=$3 save=$4
  local modarg=""
  if [ -n "$mods" ]; then modarg="--mod-directory /in/$mods"; fi
  docker run --rm --name "foreman-e3-1c-$name" --entrypoint sh \
    -v "$HERE:/in:ro" -v "$OUT:/out" "$img" -c \
    "/opt/factorio/bin/x64/factorio --create /out/$save --map-gen-settings /in/mapgen.json $modarg && chown $U:$G /out/$save"
}

run 11-vanilla "$IMG11" "" 1.1.110-vanilla.zip
run 11-mod "$IMG11" mods11 1.1.110-testmod.zip
run 20-sa "$IMG20" "" 2.0.77-space-age.zip
run 20-nosa "$IMG20" mods20-nosa 2.0.77-no-space-age.zip
run 21-sa "$IMG21" "" 2.1.24-space-age.zip
run 21-nosa "$IMG21" mods20-nosa 2.1.24-no-space-age.zip
run 21-mod "$IMG21" mods21-sa-mod 2.1.24-space-age-testmod.zip
