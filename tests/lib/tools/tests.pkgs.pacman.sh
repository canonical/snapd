#!/bin/sh

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
        *)
            echo "$1"
            ;;
    esac
}

cmd_install() {
    set -x
    pacman -S --noconfirm "$@"
    set +x
}

cmd_remove() {
    set -x
    pacman -Rn "$@"
    set +x
}
