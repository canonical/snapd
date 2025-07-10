#!/bin/bash

pkg=$1
rpm_dir=$(rpm --eval "%_topdir")
rm -r "$rpm_dir"/*

base_version="$(head -1 debian/changelog | awk -F '[()]' '{print $2}')"
version="1337.$base_version"
packaging_path=packaging/"$pkg"

sed -i -e "s/^Version:.*$/Version: $version/g" "$packaging_path/snapd.spec"

mkdir -p "$rpm_dir/SOURCES"
cp "$packaging_path"/* "$rpm_dir/SOURCES/"
mkdir vendor

./packaging/pack-source -v "$version" -o "$rpm_dir/SOURCES"
rpmbuild --with testkeys -bs "$rpm_dir/SOURCES/snapd.spec"