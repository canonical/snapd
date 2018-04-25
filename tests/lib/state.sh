#!/bin/bash

set -eux

SNAPD_STATE_PATH="$SPREAD_PATH/snapd-state"

get_boot_path() {
	BOOT=""
    if ls /boot/uboot/*; then
        BOOT="/boot/uboot/"
    elif ls /boot/grub/*; then
        BOOT="/boot/grub/"
    else
        echo "Cannot determine bootdir in /boot:"
        ls /boot
        exit 1
    fi
}

save_all_snap_state() {
	get_boot_path
    mkdir -p "$SNAPD_STATE_PATH"
    cp -rf /var/lib/snapd "$SNAPD_STATE_PATH/snapdlib"
    cp -rf "$BOOT" "$SNAPD_STATE_PATH/boot"
    cp -f /etc/systemd/system/snap-*core*.mount "$SNAPD_STATE_PATH"
}

restore_all_snap_state() {
    # we need to ensure that we also restore the boot environment
    # fully for tests that break it
	get_boot_path
	rm -rf /var/lib/snapd/*
    cp -rf "$SNAPD_STATE_PATH"/snapdlib/* /var/lib/snapd
    cp -rf "$SNAPD_STATE_PATH"/boot/* "$BOOT"
    cp -f "$SNAPD_STATE_PATH"/snap-*core*.mount  /etc/systemd/system
}
