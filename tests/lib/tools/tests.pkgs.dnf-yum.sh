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
            if [ "$(command -v dnf)" != "" ]; then
                echo "robotfindskitten"
            else
                echo "libXft"
            fi
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
    set -x
    # shellcheck disable=SC2068
    if [ "$(command -v dnf)" != "" ]; then
        dnf install -y $@
    else
        yum install -y $@
    fi
    set +x
}

cmd_is_installed() {
    set -x
    rpm -qi "$1" >/dev/null 2>&1
    set +x
}

cmd_query() {
    set -x
    if [ "$(command -v dnf)" != "" ]; then
        dnf info "$1"
    else
        yum info "$1"
    fi
    set +x
}

cmd_remove() {
    set -x
    # shellcheck disable=SC2068
    if [ "$(command -v dnf)" != "" ]; then
        dnf remove -y $@
    else
        yum remove -y $@
    fi
    set +x
}
