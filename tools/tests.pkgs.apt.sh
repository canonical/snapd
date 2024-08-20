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
        test-snapd-pkg-3)
            if os.query is-debian || os.query is-trusty; then
                echo cpp:i386
            elif os.query is-xenial || os.query is-bionic; then
                echo cpp-5:i386
            else
                echo cpp-9:i386
            fi
            ;;
        *)
            echo "$1"
            ;;
    esac
}

cmd_install() {
    apt-get update

    local APT_FLAGS="--yes"
    while [ -n "$1" ]; do
        case "$1" in
            --no-install-recommends)
                APT_FLAGS="$APT_FLAGS --no-install-recommends"
                shift
                ;;
            *)
                break
                ;;
        esac
    done
    # shellcheck disable=SC2086
    apt-get install $APT_FLAGS "$@"
}

cmd_is_installed() {
    dpkg -l "$1" | grep -E "ii +$1" >/dev/null 2>&1
}

cmd_query() {
    apt-cache policy "$1"
}

cmd_list_installed() {
    apt list --installed | cut -d ' ' -f 1,3 | sed -e 's@/.*\s@:@g' | sort
}

cmd_remove() {
    # Allow removing essential packages, that may get installed when using i386
    # packages on amd64 system. Normally they would be really essential but in
    # this case they are not really as essential.
    local REMOVE_FLAGS="--allow-remove-essential"
    if os.query is-trusty; then
        REMOVE_FLAGS=""
    fi
    # shellcheck disable=SC2086
    apt-get remove --yes $REMOVE_FLAGS "$@"
}
