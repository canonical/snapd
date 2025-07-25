#!/bin/bash

set -e

pkg=$1
rpm_dir=$(rpm --eval "%_topdir")

base_version="$(head -1 debian/changelog | awk -F '[()]' '{print $2}')"
version="1337.$base_version"
packaging_path=packaging/"$pkg"

sed -i -e "s/^Version:.*$/Version: $version/g" "$packaging_path/snapd.spec"
sed -i -e "s/^BuildRequires:.*fakeroot/# BuildRequires: fakeroot/" "$packaging_path/snapd.spec"

mkdir -p "$rpm_dir/SOURCES"
cp "$packaging_path"/* "$rpm_dir/SOURCES/"

pack_args=
if [[ "$pkg" =~ "opensuse" ]]; then
    pack_args=-s
fi

./packaging/pack-source -v "$version" -o "$rpm_dir/SOURCES" $pack_args
rpmbuild --with testkeys -bs "$rpm_dir/SOURCES/snapd.spec"