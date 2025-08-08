#!/bin/bash

set -e

usage() {
    echo "Usage: $0 --pkg-dir <package> --vendor-tar-dir <vendor_tar_dir> --config-file <config_name>"
    exit 1
}

pkg_dir=""
vendor_tar_dir=""
config_file=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --pkg-dir)
            pkg_dir="$2"
            shift 2
            ;;
        --vendor-tar-dir)
            vendor_tar_dir="$2"
            shift 2
            ;;
        --config-file)
            config_file="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

# Check required arguments
if [ -z "$pkg_dir" ] || [ -z "$vendor_tar_dir" ] || [ -z "$config_file" ]; then
    usage
fi

packaging_path=packaging/"$pkg_dir"
src_dir=/tmp/sources
mkdir "$src_dir"

cp "$packaging_path"/* "$src_dir"
cp "$vendor_tar_dir"/* "$src_dir"

mock -r "$config_file" --install git

mock -r "$config_file" \
    --no-clean \
    --no-cleanup-after \
    --buildsrpm \
    --with testkeys \
    --spec "$src_dir/snapd.spec" \
    --sources "$src_dir" \
    --resultdir /home/mockbuilder/builds

mock -r "$config_file" \
    --no-clean \
    --no-cleanup-after \
    --enable-network \
    --nocheck \
    --with testkeys \
    --resultdir /home/mockbuilder/builds \
    /home/mockbuilder/builds/snapd*.src.rpm
