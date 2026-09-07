#!/bin/sh
# ensure-version.sh - make sure snapd version files exist in the source tree.
#
# Release source tarballs produced by packaging/pack-source already carry
# snapdtool/version_generated.go (and cmd/VERSION, data/info), in which case
# this script does nothing. When building from a git checkout instead (as the
# snapd CI test harness does), the file is absent and is generated with
# mkversion.sh, which derives the version from git history or the Debian
# changelog. Distribution packaging rules/spec files do not call this: they
# build from a pack-source tarball that already carries the version files.
#
# usage: ensure-version.sh [srcdir]
set -e

srcdir="${1:-.}"

if [ -f "$srcdir/snapdtool/version_generated.go" ]; then
    exit 0
fi

# mkversion.sh runs `go run ./asserts/info` to compute the assertion formats in
# data/info, so it needs a Go toolchain on PATH. On some distributions Go is
# installed in a versioned directory (e.g. /usr/lib/go-1.23/bin) that is not on
# the default PATH; add those to PATH so the git-checkout build works there too.
if ! command -v go >/dev/null 2>&1; then
    for godir in /usr/lib/go-*/bin; do
        if [ -x "$godir/go" ]; then
            PATH="$godir:$PATH"
        fi
    done
    export PATH
fi

exec "$srcdir/mkversion.sh"
