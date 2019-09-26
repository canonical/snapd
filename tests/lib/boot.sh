#!/bin/bash

bootenv() {
    if [ $# -eq 0 ]; then
        snap debug boot-vars
    else
        snap debug boot-vars | grep "^$1" | sed "s/^${1}=//"
    fi
}

GRUB_EDITENV=grub-editenv
GRUBENV_FILE=/boot/grub/grubenv
case "$SPREAD_SYSTEM" in
    fedora-*|opensuse-*|amazon-*|centos-*)
        GRUB_EDITENV=grub2-editenv
        ;;
esac

# unset the given var from boot configuration
bootenv_unset() {
    local var="$1"

    if command -v "$GRUB_EDITENV" >/dev/null; then
        "$GRUB_EDITENV" /boot/grub/grubenv unset "$var"
    elif [ -s "$GRUBENV_FILE" ]; then
        sed -i "/^$var=/d" "$GRUBENV_FILE"
    else
        fw_setenv "$var"
    fi
}

get_boot_path() {
    if [ -f /boot/uboot/uboot.env ]; then
        echo "/boot/uboot/"
    elif [ -f /boot/grub/grubenv ]; then
        echo "/boot/grub/"
    else
        echo "Cannot determine boot path"
        ls -alR /boot
        exit 1
    fi
}

wait_core_post_boot() {
    # booted
    while [ "$(bootenv snap_mode)" != "" ]; do
        sleep 1
    done
}
