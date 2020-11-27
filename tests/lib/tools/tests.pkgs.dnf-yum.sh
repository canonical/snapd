#!/bin/sh

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
        test-snapd-pkg)
            echo "robotfindskitten"
            ;;
        *)
            echo "$1"
            ;;
    esac
}

cmd_install() {
    set -x
    if [ "$(command -v dnf)" != "" ]; then
        dnf install -y "$@"
    else
        yum install -y "$@"
    fi
    set +x
}

cmd_is_installed() {
    set -x
    rpm -qi "$@" >/dev/null 2>&1
    set +x
}

cmd_query() {
    set -x
    if [ "$(command -v dnf)" != "" ]; then
        dnf info "$@"
    else
        yum info "$@"
    fi
    set +x
}

cmd_remove() {
    set -x
    if [ "$(command -v dnf)" != "" ]; then
        dnf remove -y "$@"
    else
        yum remove -y "$@"
    fi
    set +x
}
