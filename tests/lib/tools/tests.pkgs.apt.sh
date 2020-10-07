#!/bin/sh

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
        *)
            echo "$1"
            ;;
    esac
}

cmd_install() {
    set -x
    apt-get install --yes "$@"
    set +x
}

cmd_remove() {
    set -x
    apt-get remove --yes "$@"
    set +x
}
