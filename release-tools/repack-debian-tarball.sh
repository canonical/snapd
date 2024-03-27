#!/bin/sh
#doc# This script is used to re-pack the "orig" tarball from the Debian package
#doc# into a suitable upstream release. There are two changes applied: The Debian
#doc# tarball contains the directory snapd.upstream/ which needs to become
#doc# snapd-$VERSION. The Debian tarball contains the vendor/ directory which must
#doc# be removed from one of those.
#doc#
#doc# Example usage, using tarball from the archive or from the image ppa:
#doc#
#doc# $ wget https://launchpad.net/ubuntu/+archive/primary/+files/snapd_2.31.2.tar.xz
#doc# $ wget https://launchpad.net/~snappy-dev/+archive/ubuntu/image/+files/snapd_2.32.1.tar.xz
#doc#
#doc# $ repack-debian-tarball.sh snapd_2.31.2.tar.xz
#doc#
#doc# This will produce three files that need to be added to the github release page:
#doc#
#doc# - snapd_2.31.2.no-vendor.tar.xz
#doc# - snapd_2.31.2.vendor.tar.xz
#doc# - snapd_2.31.2.only-vendor.xz

set -ue

# Get the filename from argv[1]
debian_tarball="${1:-}"
if [ "$debian_tarball" = "" ]; then
	echo "Usage: repack-debian-tarball.sh <snapd-debian-tarball>"
	echo
	grep -e '^#doc#' "$0" | cut -b 7-
	exit 1
fi

if [ ! -f "$debian_tarball" ]; then
	echo "cannot operate on $debian_tarball, no such file"
	exit 1
fi

# Extract the upstream version from the filename.
# For example: snapd_2.31.2.tar.xz => 2.32.2
# NOTE: There is no dash (-) in the version because snapd is a native Debian package.
upstream_version="$(echo "$debian_tarball" | cut -d _ -f 2 | sed -e 's/\.tar\..*//')"

# Scratch directory is where the original tarball is unpacked.
scratch_dir="$(mktemp -d)"
cleanup() {
	rm -rf "$scratch_dir"
}
trap cleanup EXIT

# Unpack the original with fakeroot (to preserve ownership of files).
fakeroot tar \
	--auto-compress \
	--extract \
	--file="$debian_tarball" \
	--directory="$scratch_dir/"

# Top-level directory may be either snappy.upstream or snapd.upstream, because
# of small differences between the release manager's laptop and desktop machines.
if [ -d "$scratch_dir/snapd.upstream" ]; then
	top_dir=snapd.upstream
elif [ -d "$scratch_dir/snappy.upstream" ]; then
	top_dir=snappy.upstream
elif [ -d "$scratch_dir/snapd-${upstream_version}" ]; then
	top_dir=snapd-${upstream_version}
else
	echo "Unexpected contents of given tarball, expected snap{py,d}.upstream/"
	exit 1
fi

# Pack a fully copy with vendor tree
fakeroot tar \
	--create \
	--transform="s/$top_dir/snapd-$upstream_version/" \
	--file=snapd_"$upstream_version".vendor.tar.xz \
	--auto-compress \
	--directory="$scratch_dir/" "$top_dir"

# Pack a copy without vendor tree
fakeroot tar \
	--create \
	--transform="s/$top_dir/snapd-$upstream_version/" \
	--exclude='snapd*/vendor/*' \
	--exclude='snapd*/c-vendor/*' \
	--file=snapd_"$upstream_version".no-vendor.tar.xz \
	--auto-compress \
	--directory="$scratch_dir/" "$top_dir"

# Pack a copy of the vendor tree
fakeroot tar \
	--create \
	--transform="s/$top_dir/snapd-$upstream_version/" \
	--file=snapd_"$upstream_version".only-vendor.tar.xz \
	--auto-compress \
	--directory="$scratch_dir/" "$top_dir"/vendor/ "$top_dir"/c-vendor/
