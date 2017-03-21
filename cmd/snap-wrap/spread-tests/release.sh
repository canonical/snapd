#!/bin/sh
# This script creates a new release tarball
set -xue

# Sanity check, are we in the top-level directory of the tree?
test -f configure.ac || ( echo 'this script must be executed from the top-level of the tree' && exit 1)

# Record where the top level directory is
top_dir=$(pwd)

# Create source distribution tarball and place it in the top-level directory.
create_dist_tarball() {
    # Load the version number from a dedicated file
    local pkg_version=
    pkg_version="$(cat "$top_dir/VERSION")"

    # Ensure that build system is up-to-date and ready
    autoreconf -f -i
    # XXX: This fixes somewhat odd error when configure below (in an empty directory) fails with:
    # configure: error: source directory already configured; run "make distclean" there first
    test -f Makefile && make distclean

    # Create a scratch space to run configure
    scratch_dir="$(mktemp -d)"
    trap 'rm -rf "$scratch_dir"' EXIT

    # Configure the project in a scratch directory
    cd "$scratch_dir"
    "$top_dir/configure" --prefix=/usr

    # Create the distribution tarball
    make dist

    # Ensure we got the tarball we were expecting to see
    test -f "snap-wrap-$pkg_version.tar.gz"

    # Move it to the top-level directory
    mv "snap-wrap-$pkg_version.tar.gz" "$top_dir/"
}

create_dist_tarball
