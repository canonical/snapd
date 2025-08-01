#!/bin/bash

set -e

snapd_dir=$1
user=$2
build_dir=$3

cd "$snapd_dir"

version=
for file in "$build_dir"/snapd_*.vendor.tar.xz; do
        version="${file##*/snapd_}"
        version="${version%.vendor.tar.xz}"
        break
done
cp -av packaging/arch/* "$build_dir"
sed -i -e "s/pkgver=.*/pkgver=$version/" "$build_dir"/PKGBUILD
chown -R "$user":"$user" "$build_dir"
unshare -n -- \
        su -l -c "cd $build_dir && WITH_TEST_KEYS=1 makepkg -f --nocheck" "$user"