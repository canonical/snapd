#!/bin/sh

DISTRO_BUILD_DEPS=""

case "$SPREAD_SYSTEM" in
    debian-*|ubuntu-*)
        DISTRO_BUILD_DEPS="build-essential curl devscripts expect gdebi-core jq rng-tools git netcat-openbsd"
        ;;
    *)
        ;;
esac
