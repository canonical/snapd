#!/bin/bash

set -e

vendor_tar_dir=$1
res_dir=$2

rpm_dir=$(rpm --eval "%_topdir")
packaging_path=packaging/opensuse-15.6

version=
for file in "$vendor_tar_dir"/snapd_*.vendor.tar.xz; do
        version="${file##*/snapd_}"
        version="${version%.vendor.tar.xz}"
        break
done

sed -i -e "s/^Version:.*$/Version: $version/g" "$packaging_path/snapd.spec"

mkdir -p "$rpm_dir/SOURCES"
cp "$packaging_path"/* "$rpm_dir/SOURCES/"
cp "$vendor_tar_dir"/* "$rpm_dir/SOURCES/"

# Build our source package
unshare -n -- \
        rpmbuild --with testkeys -bs "$rpm_dir/SOURCES/snapd.spec"

# And now build our binary package
unshare -n -- \
        rpmbuild \
        --with testkeys \
        --nocheck \
        -ba \
        "$rpm_dir/SOURCES/snapd.spec"

find "$rpm_dir"/RPMS -name '*.rpm' -exec cp {} "$res_dir" \;