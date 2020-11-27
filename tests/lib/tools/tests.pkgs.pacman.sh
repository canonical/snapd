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
        test-snapd-pkg)
            echo "curseofwar"
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

cmd_is_installed() {
    set -x
    pacman -Qi "$@" >/dev/null 2>&1
    set +x
}

cmd_query() {
    set -x
    pacman -Si "$@"
    set +x
}

cmd_remove() {
    set -x
    pacman -Rnsc --noconfirm "$@"
    set +x
}
