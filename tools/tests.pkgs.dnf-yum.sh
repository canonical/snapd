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
    local CMD="dnf"
    if [ -z "$(command -v dnf)" ]; then
        CMD="yum"
    fi
    local DNF_YUM_FLAGS="-y"

    while [ -n "$1" ]; do
        case "$1" in
            --no-install-recommends)
                DNF_YUM_FLAGS="$DNF_YUM_FLAGS --setopt=install_weak_deps=False"
                shift
                ;;
            *)
                break
                ;;
        esac
    done

    # shellcheck disable=SC2068,SC2086
    $CMD install $DNF_YUM_FLAGS $@
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
