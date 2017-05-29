#!/bin/sh

# Default applies for: Ubuntu, Debian
export SNAPMOUNTDIR=/snap
export LIBEXECDIR=/usr/lib

# For all other systems we need to change some directory paths
case "$SPREAD_SYSTEM" in
    fedora-*)
        export SNAPMOUNTDIR=/var/lib/snapd/snap
        export LIBEXECDIR=/usr/libexec
        ;;
    *)
        ;;
esac
