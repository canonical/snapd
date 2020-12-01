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
            echo "curseofwar"
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
    set -x
    # shellcheck disable=SC2068
    pacman -S --noconfirm $@
    set +x
}

cmd_is_installed() {
    set -x
    pacman -Qi "$1" >/dev/null 2>&1
    set +x
}

cmd_query() {
    set -x
    pacman -Si "$1"
    set +x
}

cmd_remove() {
    set -x
    # shellcheck disable=SC2068
    pacman -Rnsc --noconfirm $@
    set +x
}
