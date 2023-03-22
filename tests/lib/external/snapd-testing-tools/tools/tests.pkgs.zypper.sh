#!/bin/bash

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
        test-snapd-pkg-1)
            echo "nudoku"
            ;;
        test-snapd-pkg-2)
            echo "system-user-games"
            ;;
        *)
            echo "$1"
            ;;
    esac
}

cmd_install() {
    # shellcheck disable=SC2068
    zypper install -y $@
}

cmd_is_installed() {
    rpm -qi "$1" >/dev/null 2>&1
}

cmd_query() {
    zypper info "$1"
}

cmd_list_installed() {
    rpm -qa | sort
}

cmd_remove() {
    # shellcheck disable=SC2068
    zypper remove -y $@
}
