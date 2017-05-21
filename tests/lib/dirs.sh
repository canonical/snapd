#!/bin/sh

# Default applies for: Ubuntu, Debian
SNAPMOUNTDIR=/snap
LIBEXECDIR=/usr/lib

# For all other systems we need to change some directory paths
case "$SPREAD_SYSTEM" in
    fedora-*)
        SNAPMOUNTDIR=/var/lib/snapd/snap
        LIBEXECDIR=/usr/libexec
        ;;
    *)
        ;;
esac
