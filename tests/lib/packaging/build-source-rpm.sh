#!/bin/bash

set -e

pkg=$1
vendor_tar_dir=$2
rpm_dir=$(rpm --eval "%_topdir")

version=$(ls "$vendor_tar_dir" | grep -oP '(?<=snapd_).*(?=\.vendor\.tar\.xz)')
packaging_path=packaging/"$pkg"

sed -i -e "s/^Version:.*$/Version: $version/g" "$packaging_path/snapd.spec"
sed -i -e "s/^BuildRequires:.*fakeroot/# BuildRequires: fakeroot/" "$packaging_path/snapd.spec"

mkdir -p "$rpm_dir/SOURCES"
cp "$packaging_path"/* "$rpm_dir/SOURCES/"
cp "$vendor_tar_dir"/* "$rpm_dir/SOURCES/"

rpmbuild --with testkeys -bs "$rpm_dir/SOURCES/snapd.spec"