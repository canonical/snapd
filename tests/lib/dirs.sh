#!/bin/sh

export SNAPMOUNTDIR=/snap
export LIBEXECDIR=/usr/lib

case "$SPREAD_SYSTEM" in
    fedora-*)
        export SNAPMOUNTDIR=/var/lib/snapd/snap
        export LIBEXECDIR=/usr/libexec
        ;;
    *)
        ;;
esac
