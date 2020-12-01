#!/bin/bash

remap_one() {
    case "$1" in
        man)
            if [ "$(cat /etc/os-release && echo "$ID")" = debian ]; then
                echo "man-db"
            else
                echo "$1"
            fi
            ;;
        printer-driver-cups-pdf)
            if [ "$(cat /etc/os-release && echo "$ID")" = debian ] || [ "$(cat /etc/os-release && echo "$ID/$ID_VERSION")" = ubuntu/14.04 ]; then
                echo "cups-pdf"
            else
                echo "$1"
            fi
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
    apt-get install --yes $@
    set +x
}

cmd_is_installed() {
    set -x
    dpkg -S "$1" >/dev/null 2>&1
    set +x
}

cmd_query() {
    set -x
    apt-cache policy "$1"
    set +x
}

cmd_remove() {
    set -x
    # shellcheck disable=SC2068
    apt-get remove --yes $@
    set +x
}
