#!/bin/sh

DISTRO_BUILD_DEPS=""

case "$SPREAD_SYSTEM" in
    debian-*)
        ;&
    ubuntu-*)
        DISTRO_BUILD_DEPS="build-essential curl devscripts expect gdebi-core jq rng-tools git netcat-openbsd"
        ;;
    fedora-*)
        DISTRO_BUILD_DEPS="mock git expect curl golang rpm-build redhat-lsb-core"
        ;;
    opensuse-*)
        DISTRO_BUILD_DEPS="git"
        ;;
    *)
        ;;
esac