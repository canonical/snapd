#!/bin/bash

export SNAP_MOUNT_DIR=/snap
export LIBEXECDIR=/usr/lib
export MEDIA_DIR=/media

case "$SPREAD_SYSTEM" in
    fedora-*|amazon-*|centos-*)
        export SNAP_MOUNT_DIR=/var/lib/snapd/snap
        export LIBEXECDIR=/usr/libexec
        export MEDIA_DIR=/run/media
        ;;
    arch-*)
        export SNAP_MOUNT_DIR=/var/lib/snapd/snap
        export MEDIA_DIR=/run/media
        ;;
    opensuse-*)
        export SNAP_MOUNT_DIR=/snap
        export MEDIA_DIR=/run/media
        if os.query is-opensuse-tumbleweed; then
            # Tumbleweed since snapshot 20200827 is using /usr/libexec as libexecdir
            export LIBEXECDIR=/usr/libexec
        fi
        ;;
    *)
        ;;
esac
