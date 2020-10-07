#!/bin/sh

remap_one() {
    case "$1" in
        python3-yaml)
            echo "python3-PyYAML"
            ;;
        dbus-x11)
            echo "dbus-1-x11"
            ;;
        printer-driver-cups-pdf)
            echo "cups-pdf"
            ;;
        python3-dbus)
            # In OpenSUSE Leap 15, this is renamed to python3-dbus-python
            echo "dbus-1-python3"
            ;;
        python3-gi)
            echo "python3-gobject"
            ;;
        *)
            echo "$1"
            ;;
    esac
}

cmd_install() {
    set -x
    zypper install -y "$@"
    set +x
}

cmd_remove() {
    set -x
    zypper remove -y "$@"
    set +x
}
