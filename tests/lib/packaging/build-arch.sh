#!/bin/bash

set -e

snapd_dir=$1
user=$2
build_dir=$3

cd "$snapd_dir"
go mod vendor
su -c "cd $snapd_dir/c-vendor && ./vendor.sh" "$user"


base_version="$(head -1 debian/changelog | awk -F '[()]' '{print $2}')"
version="1337.$base_version"
mkdir -p "$build_dir"
cp -av packaging/arch/* "$build_dir"
./packaging/pack-source -v "$version" -o "$build_dir" -s
sed -i -e "s/pkgver=.*/pkgver=$version/" "$build_dir"/PKGBUILD
chown -R "$user":"$user" "$build_dir"
unshare -n -- \
        su -l -c "cd $build_dir && WITH_TEST_KEYS=1 makepkg -f --nocheck" "$user"