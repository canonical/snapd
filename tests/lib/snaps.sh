#!/bin/sh

install_local() {
    local SNAP_FILE="$TESTSLIB/snaps/$1/$1_1.0_all.snap"
    local SNAP_DIR=$(dirname "$SNAP_FILE")
    if [ ! -f "$SNAP_FILE" ]; then
        snapbuild "$SNAP_DIR" "$SNAP_DIR"
    fi
    snap install --dangerous "$SNAP_FILE"
}

install_local_devmode() {
    local SNAP_FILE="$TESTSLIB/snaps/$1/$1_1.0_all.snap"
    local SNAP_DIR=$(dirname "$SNAP_FILE")
    if [ ! -f "$SNAP_FILE" ]; then
        snapbuild "$SNAP_DIR" "$SNAP_DIR"
    fi
    snap install --devmode --dangerous "$SNAP_FILE"
}
