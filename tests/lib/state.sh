#!/bin/bash

SNAPD_STATE_PATH="$SPREAD_PATH"/snapd-state
SNAPD_STATE_FILE="$SPREAD_PATH"/snapd-state.tar

# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"

# shellcheck source=tests/lib/boot.sh
. "$TESTSLIB/boot.sh"

delete_snapd_state() {
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-16-* ]]; then
        rm -rf "$SNAPD_STATE_PATH"
    else
        rm -f "$SNAPD_STATE_FILE"
    fi
}

save_snapd_state() {
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-16-* ]]; then
        boot_path="$(get_boot_path)"
        test -n "$boot_path" || return 1

        mkdir -p "$SNAPD_STATE_PATH" "$SNAPD_STATE_PATH"/system-units

        # Copy the state preserving the timestamps
        cp -rfp /var/lib/snapd "$SNAPD_STATE_PATH"/snapd-lib
        cp -rf "$boot_path" "$SNAPD_STATE_PATH"/boot
        cp -f /etc/systemd/system/snap-*core*.mount "$SNAPD_STATE_PATH"/system-units
    else
        escaped_snap_mount_dir=$1
        snapd_env="/etc/environment /etc/systemd/system/snapd.service.d /etc/systemd/system/snapd.socket.d"
        snap_confine_profiles="$(ls /etc/apparmor.d/snap.core.* || true)"
        # shellcheck disable=SC2086
        tar cf "$SNAPD_STATE_FILE" \
            /var/lib/snapd \
            "$SNAP_MOUNT_DIR" \
            /etc/systemd/system/"$escaped_snap_mount_dir"-*core*.mount \
            /etc/systemd/system/multi-user.target.wants/"$escaped_snap_mount_dir"-*core*.mount \
            $snap_confine_profiles \
            $snapd_env
    fi
}

restore_snapd_state() {
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-16-* ]]; then
        # we need to ensure that we also restore the boot environment
        # fully for tests that break it
        boot_path="$(get_boot_path)"
        test -n "$boot_path" || return 1

        restore_snapd_lib
        cp -rf "$SNAPD_STATE_PATH"/boot/* "$boot_path"
        cp -f "$SNAPD_STATE_PATH"/system-units/*  /etc/systemd/system
    else
        # Purge all the systemd service units config
        rm -rf /etc/systemd/system/snapd.service.d
        rm -rf /etc/systemd/system/snapd.socket.d

        tar -C/ -xf "$SNAPD_STATE_FILE"
    fi
}

restore_snapd_lib() {
    # Clean all the state but the snaps and seed dirs. Then make a selective clean for 
    # snaps and seed dirs leaving the .snap files which then are going to be synchronized.
    find /var/lib/snapd/* -maxdepth 0 ! \( -name 'snaps' -o -name 'seed' \) -exec rm -rf {} +

    # Copy the whole state but the snaps and seed dirs
    find "$SNAPD_STATE_PATH"/snapd-lib/* -maxdepth 0 ! \( -name 'snaps' -o -name 'seed' \) -exec cp -rf {} /var/lib/snapd \;

    # Synchronize snaps and seed directories. The this is done separately in order to avoid copying 
    # the snap files due to it is a heavy task and take most of the time of the restore phase.
    rsync -av --delete "$SNAPD_STATE_PATH"/snapd-lib/snaps /var/lib/snapd
    rsync -av --delete "$SNAPD_STATE_PATH"/snapd-lib/seed /var/lib/snapd
}
