#!/bin/sh

export SNAP_MOUNT_DIR=/snap
export LIBEXECDIR=/usr/lib
export MEDIA_DIR=/media

case "$SPREAD_SYSTEM" in
    fedora-*)
        export SNAP_MOUNT_DIR=/var/lib/snapd/snap
        export LIBEXECDIR=/usr/libexec
        export MEDIA_DIR=/run/media
        ;;
    *)
        ;;
esac
