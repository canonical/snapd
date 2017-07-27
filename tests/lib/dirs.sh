#!/bin/sh

export SNAP_MOUNT_DIR=/snap
export LIBEXECDIR=/usr/lib

case "$SPREAD_SYSTEM" in
    fedora-*)
        export SNAP_MOUNT_DIR=/var/lib/snapd/snap
        export LIBEXECDIR=/usr/libexec
        ;;
    *)
        ;;
esac
