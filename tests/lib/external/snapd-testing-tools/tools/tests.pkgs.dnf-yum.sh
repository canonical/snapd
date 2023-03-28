#!/bin/bash

remap_one() {
    case "$1" in
        xdelta3)
            echo "xdelta"
            ;;
        openvswitch-switch)
            echo "openvswitch"
            ;;
        printer-driver-cups-pdf)
            echo "cups-pdf"
            ;;
        python3-gi)
            echo "python3-gobject"
            ;;
        test-snapd-pkg-1)
            echo "freeglut"
            ;;
        test-snapd-pkg-2)
            echo "texlive-base"
            ;;
        *)
            echo "$1"
            ;;
    esac
}

cmd_install() {
    # shellcheck disable=SC2068
    if [ "$(command -v dnf)" != "" ]; then
        dnf install -y $@
    else
        yum install -y $@
    fi
}

cmd_is_installed() {
    rpm -qi "$1" >/dev/null 2>&1
}

cmd_query() {
    if [ "$(command -v dnf)" != "" ]; then
        dnf info "$1"
    else
        yum info "$1"
    fi
}

cmd_list_installed() {
    rpm -qa | sort
}

cmd_remove() {
    # shellcheck disable=SC2068
    if [ "$(command -v dnf)" != "" ]; then
        dnf remove -y $@
    else
        yum remove -y $@
    fi
}
