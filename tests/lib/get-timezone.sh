#!/bin/bash

# This script provides a fallback for systems where `timedatectl show
# -p Timezone --value` does not work

set -eu

if timezone="$(timedatectl show -p Timezone --value 2>/dev/null)"; then
    echo "${timezone}"
    exit 0
fi

read_abs_symlink() {
    local target
    local fromdir

    target=$(readlink "${1}")
    case "${target}" in
        /*)
            echo "${target}"
            ;;
        *)
            fromdir=$(dirname "${1}")
            realpath "${fromdir}/${target}"
            ;;
    esac
}

read_localtime() {
    local target

    if ! [ -L "${1}" ]; then
        echo "${1} is not a symlink" 1>&2
        exit 1
    fi

    target="$(read_abs_symlink "${1}")"

    case "${target}" in
        /usr/share/zoneinfo/*)
            echo "${target#/usr/share/zoneinfo/}"
            ;;
        *)
            read_localtime "${target}"
            ;;
    esac
}

if [ -L /etc/localtime ]; then
    read_localtime /etc/localtime
elif [ -f /etc/timezone ]; then
    # Initial /etc/localtime from postinst on old Debian/Ubuntu copies
    # /etc/localtime instead of linking. Trying to recover the
    # timezone from /etc/timezone.
    cat /etc/timezone
fi
