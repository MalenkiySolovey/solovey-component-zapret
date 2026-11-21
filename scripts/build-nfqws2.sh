#!/usr/bin/env bash
# Builds an immutable external source input; never installs or runs the engine.
set -euo pipefail
test "$#" -eq 1
root=$(cd -- "$(dirname -- "$0")/.." && pwd -P)
read -r commit checksum epoch < <(node -e '
  const pin = JSON.parse(require("node:fs").readFileSync(process.argv[1]));
  if (!/^[a-f0-9]{40}$/.test(pin.commit) || !/^[a-f0-9]{64}$/.test(pin.archiveSHA256) || !Number.isSafeInteger(pin.sourceDateEpoch) || pin.sourceDateEpoch < 1) process.exit(1);
  console.log(pin.commit, pin.archiveSHA256, pin.sourceDateEpoch);
' "$root/upstream/zapret2.json")
out=$1
test ! -e "$out"
scratch=$(mktemp -d)
trap 'rm -r -- "$scratch"' EXIT
curl --fail --silent --show-error --location --proto '=https' --max-time 90 --max-filesize 8388608 \
  "https://codeload.github.com/bol-van/zapret2/tar.gz/$commit" -o "$scratch/source.tar.gz"
printf '%s  %s\n' "$checksum" "$scratch/source.tar.gz" | sha256sum -c -
mkdir "$scratch/source"
tar -xzf "$scratch/source.tar.gz" --strip-components=1 -C "$scratch/source"
export SOURCE_DATE_EPOCH="$epoch" LC_ALL=C TZ=UTC
gcc -std=c99 -Wall -Wextra -Werror -fPIC -c "$root/packaging/nfqueue-policy.c" \
  -o "$scratch/source/nfq2/solovey-nfqueue-policy.o"
make -C "$scratch/source/nfq2" CC=gcc LUA_JIT=0 LUA_VER=5.4 \
  LIBS=solovey-nfqueue-policy.o 'LDFLAGS_PIE=-pie -Wl,--wrap=nfq_set_queue_flags'
mkdir -- "$out"
mkdir -- "$out/lua"
install -m 0755 "$scratch/source/nfq2/nfqws2" "$out/nfqws2"
install -m 0644 "$scratch/source/docs/LICENSE.txt" "$out/LICENSE.upstream"
install -m 0644 "$scratch/source/lua/"{zapret-lib,zapret-antidpi}.lua "$out/lua/"
install -m 0644 "$root/upstream/zapret2.json" "$out/source.json"
install -m 0644 "$root/packaging/nfqueue-policy.c" "$out/nfqueue-policy.c"
(
  cd -- "$out"
  sha256sum nfqws2 LICENSE.upstream lua/*.lua source.json nfqueue-policy.c > SHA256SUMS
  cat SHA256SUMS
  gcc --version | head -n 1
  pkg-config --modversion libnetfilter_queue libnfnetlink libmnl zlib lua5.4
)
