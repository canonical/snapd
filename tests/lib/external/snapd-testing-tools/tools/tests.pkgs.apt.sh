#!/bin/bash

remap_one() {
    case "$1" in
        man)
            if os.query is-debian; then
                echo "man-db"
            else
                echo "$1"
            fi
            ;;
        printer-driver-cups-pdf)
            if os.query is-debian || os.query is-trusty; then
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
    apt-get update
    # shellcheck disable=SC2068
    apt-get install --yes $@
}

cmd_is_installed() {
    dpkg -S "$1" >/dev/null 2>&1
}

cmd_query() {
    apt-cache policy "$1"
}

cmd_list_installed() {
    apt list --installed | cut -d/ -f1 | sort
}

cmd_remove() {
    # shellcheck disable=SC2068
    apt-get remove --yes $@
}
