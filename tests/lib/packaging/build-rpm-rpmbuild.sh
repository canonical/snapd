#!/bin/bash

set -e

usage() {
    echo "Usage: $0 --vendor-tar-dir <vendor_tar_dir> --build-dir <build_dir> --pkg-dir <pkg_dir>"
    exit 1
}

vendor_tar_dir=""
build_dir=""
pkg_dir=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --vendor-tar-dir)
            vendor_tar_dir="$2"
            shift 2
            ;;
        --build-dir)
            build_dir="$2"
            shift 2
            ;;
        --pkg-dir)
            pkg_dir="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

# Check required arguments
if [ -z "$vendor_tar_dir" ] || [ -z "$build_dir" ] || [ -z "$pkg_dir" ]; then
    usage
fi

rpm_dir=$(rpm --eval "%_topdir")

mkdir -p "$rpm_dir/SOURCES"
mv "packaging/$pkg_dir"/* "$rpm_dir/SOURCES/"
mv "$vendor_tar_dir"/* "$rpm_dir/SOURCES/"

unshare -n -- \
        rpmbuild \
        --with testkeys \
        --nocheck \
        -ba \
        "$rpm_dir/SOURCES/snapd.spec"


find "$rpm_dir"/RPMS -type f \
        ! -name '*debug*' \
        -name '*.rpm' \
        -exec mv {} "$build_dir" \;
