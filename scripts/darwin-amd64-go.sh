#!/usr/bin/env bash
# Builds a Go toolchain for the darwin/amd64 plugin library and prints its GOROOT.
#
# Stock Go keeps the current goroutine (g) in TLS slot 6 (0x30(GS)) on darwin/amd64 in every
# runtime. A c-shared Go plugin loaded into the Go host (CPA) therefore overwrites the host's g
# and the other way round, and CPA crashes. This toolchain moves the plugin's g to slot 11
# (0x58(GS)), the other slot Apple reserves. The host stays stock.
#
# Usage: scripts/darwin-amd64-go.sh <dest-dir> [go-version]
#   go-version defaults to the version of the `go` on PATH (e.g. go1.26.9).
set -euo pipefail

dest=${1:?usage: darwin-amd64-go.sh <dest-dir> [go-version]}
ver=${2:-$(go env GOVERSION)}
die() { echo "darwin-amd64-go: $*" >&2; exit 1; }

case $ver in go1.*) ;; *) die "unexpected Go version '$ver'" ;; esac
[ "$(cat "$dest/go/.tls-slot" 2>/dev/null)" = "$ver" ] && { echo "$dest/go"; exit 0; }

sum=$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' | python3 -I -c '
import json, sys
v = sys.argv[1]
for r in json.load(sys.stdin):
    for f in r["files"]:
        if r["version"] == v and f["kind"] == "source":
            print(f["sha256"])
' "$ver")
[ -n "$sum" ] || die "no source checksum published for $ver"

rm -rf "$dest" && mkdir -p "$dest"
curl -fsSL -o "$dest/src.tar.gz" "https://go.dev/dl/$ver.src.tar.gz"
echo "$sum  $dest/src.tar.gz" | shasum -a 256 -c - >/dev/null || die "$ver source checksum mismatch"
tar -xzf "$dest/src.tar.gz" -C "$dest" && rm "$dest/src.tar.gz"

# replace <file> <old> <new>: the old text must occur exactly once.
replace() {
  local f="$dest/go/$1"
  [ "$(grep -cF -- "$2" "$f")" = 1 ] || die "$1: expected exactly one '$2'"
  OLD=$2 NEW=$3 perl -0pi -e 's/\Q$ENV{OLD}\E/$ENV{NEW}/' "$f"
}
replace src/cmd/link/internal/ld/sym.go 'ctxt.Tlsoffset = 0x30' 'ctxt.Tlsoffset = 0x58'
replace src/runtime/sys_darwin_amd64.s 'MOVQ	DX, 0x30(GS)' 'MOVQ	DX, 0x58(GS)'

bootstrap=$(go env GOROOT)
(cd "$dest/go/src" && env -u GOTOOLCHAIN -u GOFLAGS -u CGO_CFLAGS -u CGO_LDFLAGS -u MACOSX_DEPLOYMENT_TARGET \
  GOROOT_BOOTSTRAP="$bootstrap" ./make.bash >"$dest/make.log" 2>&1) || { tail -40 "$dest/make.log" >&2; die "make.bash failed"; }
echo "$ver" >"$dest/go/.tls-slot"
echo "$dest/go"
