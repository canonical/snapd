#!/bin/sh
# mod-version.sh - set the full downstream package version in the version
# files of a snapd source tree.
#
# This script is the downstream counterpart of packaging/gen-version.sh. Where
# gen-version.sh bakes the upstream release version into the source tarball
# (snapdtool/version_generated.go, cmd/VERSION and data/info), distribution
# packaging calls this script to stamp the full package version (upstream +
# distribution suffix) into the two files that carry the version outside of
# the Go linker flags:
#
#   - cmd/VERSION  used by autotools (AC_INIT) for snap-confine, so that
#                  "snap-confine --version" matches the Go binaries
#   - data/info    the VERSION key only; SNAPD_APPARMOR_REEXEC and the
#                  assertion formats map are preserved from the source tree
#
# The version files must already exist, i.e. the tree is expected to be a
# release source tarball produced by packaging/pack-source (or a git checkout
# on which mkversion.sh was run). To catch desync between the packaging
# metadata (spec version, Debian changelog) and the source tarball, the given
# version must either equal the upstream version or extend it with a
# distribution suffix (separated by one of: + - ~ .); anything else is an
# error.
#
# usage: mod-version.sh <version> [srcdir]
set -eu

version="${1:-}"
srcdir="${2:-.}"

if [ -z "$version" ]; then
    echo "error: version is unset" >&2
    echo "usage: $(basename "$0") <version> [srcdir]" >&2
    exit 1
fi

if [ ! -f "$srcdir/cmd/VERSION" ] || [ ! -f "$srcdir/data/info" ]; then
    echo "error: $srcdir/cmd/VERSION or $srcdir/data/info not found; the source tree must come from packaging/pack-source (or mkversion.sh must have run)" >&2
    exit 1
fi

upstream=$(cat "$srcdir/cmd/VERSION")

case "$version" in
    "$upstream")
        ;;
    "$upstream"+* | "$upstream"-* | "$upstream"~* | "$upstream".*)
        ;;
    *)
        echo "error: version '$version' does not match upstream version '$upstream' from $srcdir/cmd/VERSION (expected it to be equal, or to extend it with a distribution suffix)" >&2
        exit 1
        ;;
esac

if ! grep -q '^VERSION=' "$srcdir/data/info"; then
    echo "error: $srcdir/data/info has no VERSION= line" >&2
    exit 1
fi

printf '%s\n' "$version" > "$srcdir/cmd/VERSION"
sed -i 's/^VERSION=.*/VERSION='"$version"'/' "$srcdir/data/info"
