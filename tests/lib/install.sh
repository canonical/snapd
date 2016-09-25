#!/bin/sh

CACHE_DIR="$TESTSLIB/snaps/cache"
LOCAL_SNAP_NAME="$1"
LOCAL_SNAP_FILE="${CACHE_DIR}/${LOCAL_SNAP_NAME}_1.0_all.snap"

mkdir -p "$CACHE_DIR"

if [ ! -f "$LOCAL_SNAP_FILE" ]; then
    snapbuild "$TESTSLIB/snaps/$LOCAL_SNAP_NAME" "$CACHE_DIR"
fi
snap install --dangerous "$LOCAL_SNAP_FILE"
