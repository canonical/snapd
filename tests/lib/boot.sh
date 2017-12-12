#!/bin/bash

GRUB_EDITENV=grub-editenv
case "$SPREAD_SYSTEM" in
    fedora-*|opensuse-*)
        GRUB_EDITENV=grub2-editenv
        ;;
esac

bootenv() {
    if [ $# -eq 0 ]; then
        if command -v "$GRUB_EDITENV" >/dev/null; then
            "$GRUB_EDITENV" list
        else
            fw_printenv
        fi
    else
        if command -v "$GRUB_EDITENV" >/dev/null; then
            "$GRUB_EDITENV" list | grep "^$1"
        else
            fw_printenv "$1"
        fi | sed "s/^${1}=//"
    fi
}

# unset the given var from boot configuration
bootenv_unset() {
    local var="$1"

    if command -v "$GRUB_EDITENV" >/dev/null; then
        "$GRUB_EDITENV" /boot/grub/grubenv unset "$var"
    else
        fw_setenv "$var"
    fi
}
