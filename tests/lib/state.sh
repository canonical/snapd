#!/bin/bash

SNAPD_STATE_PATH="$SPREAD_PATH/snapd-state"

# shellcheck source=tests/lib/boot.sh
. "$TESTSLIB/boot.sh"

save_all_snap_state() {
    local boot_path="$(get_boot_path)"
    test -n "$boot_path" || return 1

    mkdir -p "$SNAPD_STATE_PATH"
    cp -rf /var/lib/snapd "$SNAPD_STATE_PATH/snapdlib"
    cp -rf "$boot_path" "$SNAPD_STATE_PATH/boot"
    cp -f /etc/systemd/system/snap-*core*.mount "$SNAPD_STATE_PATH"
}

restore_all_snap_state() {
    # we need to ensure that we also restore the boot environment
    # fully for tests that break it
    local boot_path="$(get_boot_path)"
    test -n "$boot_path" || return 1

    rm -rf /var/lib/snapd/*
    cp -rf "$SNAPD_STATE_PATH"/snapdlib/* /var/lib/snapd
    cp -rf "$SNAPD_STATE_PATH"/boot/* "$boot_path"
    cp -f "$SNAPD_STATE_PATH"/snap-*core*.mount  /etc/systemd/system
}
