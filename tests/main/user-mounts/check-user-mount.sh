#!/bin/sh

set -eux

export XDG_RUNTIME_DIR="$1"

# Add some content to the runtime dir
rm -rf $XDG_RUNTIME_DIR/snap.test-snapd-sh
mkdir $XDG_RUNTIME_DIR/snap.test-snapd-sh
mkdir $XDG_RUNTIME_DIR/snap.test-snapd-sh/source
mkdir $XDG_RUNTIME_DIR/snap.test-snapd-sh/target
touch $XDG_RUNTIME_DIR/snap.test-snapd-sh/source/in-source
touch $XDG_RUNTIME_DIR/snap.test-snapd-sh/target/in-target

# Check target directory from sandbox
test-snapd-sh -c 'ls $XDG_RUNTIME_DIR/target'
