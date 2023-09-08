#!/bin/bash

remap_one() {
    case "$1" in
        python3-yaml)
            echo "python-yaml"
            ;;
        dbus-x11)
            # no separate dbus-x11 package in arch
            echo "dbus"
            ;;
        printer-driver-cups-pdf)
            echo "cups-pdf"
            ;;
        openvswitch-switch)
            echo "openvswitch"
            ;;
        man)
            echo "man-db"
            ;;
        python3-dbus)
            echo "python-dbus"
            ;;
        python3-gi)
            echo "python-gobject"
            ;;
        test-snapd-pkg-1)
            echo "freeglut"
            ;;
        test-snapd-pkg-2)
            echo "robotfindskitten"
            ;;
        *)
            echo "$1"
            ;;
    esac
}

cmd_install() {
    local PACMAN_FLAGS="--noconfirm"
    while [ -n "$1" ]; do
        case "$1" in
            --no-install-recommends)
                # Pacman only ever installs the required dependencies
                shift
                ;;
            *)
                break
                ;;
        esac
    done
    # shellcheck disable=SC2068,SC2086
    pacman -S $PACMAN_FLAGS $@
}

cmd_is_installed() {
    pacman -Qi "$1" >/dev/null 2>&1
}

cmd_query() {
    pacman -Si "$1"
}

cmd_list_installed() {
    pacman -Qe | awk '{ print $1 }' | sort
}

cmd_remove() {
    # shellcheck disable=SC2068
    pacman -Rnsc --noconfirm $@
}
