#!/bin/sh

install_local() {
    local SNAP_NAME="$1"
    shift;
    local SNAP_FILE="$TESTSLIB/snaps/${SNAP_NAME}/${SNAP_NAME}_1.0_all.snap"
    local SNAP_DIR=$(dirname "$SNAP_FILE")
    if [ ! -f "$SNAP_FILE" ]; then
        snapbuild "$SNAP_DIR" "$SNAP_DIR"
    fi
    snap install --dangerous "$@" "$SNAP_FILE"
}

install_local_devmode() {
    install_local "$1" --devmode
}

mksnap() {
    dir="$1"
    snap="$2"
    
    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        # trusty does not support  -Xcompression-level 1
        mksquashfs "$dir" "$snap" -comp gzip
    else
        mksquashfs "$dir" "$snap" -comp gzip -Xcompression-level 1
    fi
}
