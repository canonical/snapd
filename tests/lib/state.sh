#!/bin/bash

SNAPD_STATE_PATH="$SPREAD_PATH"/snapd-state
SNAPD_STATE_FILE="$SPREAD_PATH"/snapd-state.tar

# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"

# shellcheck source=tests/lib/boot.sh
. "$TESTSLIB/boot.sh"

delete_snapd_state() {
    rm -rf $SNAPD_STATE_PATH
    rm -f $SNAPD_STATE_FILE
}

save_classic_state() {
    escaped_snap_mount_dir=$1
    snapd_env="/etc/environment /etc/systemd/system/snapd.service.d /etc/systemd/system/snapd.socket.d"
    snap_confine_profiles="$(ls /etc/apparmor.d/snap.core.* || true)"
    # shellcheck disable=SC2086
    tar cf "$SNAPD_STATE_FILE" /var/lib/snapd "$SNAP_MOUNT_DIR" /etc/systemd/system/"$escaped_snap_mount_dir"-*core*.mount /etc/systemd/system/multi-user.target.wants/"$escaped_snap_mount_dir"-*core*.mount $snap_confine_profiles $snapd_env
}

restore_classic_state() {
    # Purge all the systemd service units config
    rm -rf /etc/systemd/system/snapd.service.d
    rm -rf /etc/systemd/system/snapd.socket.d

    tar -C/ -xf "$SNAPD_STATE_FILE"
}

save_all_snap_state() {
    local boot_path="$(get_boot_path)"
    test -n "$boot_path" || return 1

    mkdir -p "$SNAPD_STATE_PATH" "$SNAPD_STATE_PATH"/system-units

    # Copy the state preserving the timestamps
    cp -rfp /var/lib/snapd "$SNAPD_STATE_PATH"/snapd-lib
    cp -rf "$boot_path" "$SNAPD_STATE_PATH"/boot
    cp -f /etc/systemd/system/snap-*core*.mount "$SNAPD_STATE_PATH"/system-units
}

restore_all_snap_state() {
    # we need to ensure that we also restore the boot environment
    # fully for tests that break it
    local boot_path="$(get_boot_path)"
    test -n "$boot_path" || return 1

    restore_snapd_lib
    cp -rf "$SNAPD_STATE_PATH"/boot/* "$boot_path"
    cp -f "$SNAPD_STATE_PATH"/system-units/*  /etc/systemd/system
}

restore_snapd_lib() {
    mkdir -p /var/lib/snapd/snaps
    mkdir -p /var/lib/snapd/seed /var/lib/snapd/seed/snaps

    # Clean all the state but the snaps and seed dirs
    find /var/lib/snapd/* -maxdepth 0 ! \( -name 'snaps' -o -name 'seed' \) -exec rm -rf {} +
    clean_snaps_dir_state
    clean_seed_dir_state

    # Copy the whole state but the snaps and seed dirs
    find "$SNAPD_STATE_PATH"/snapd-lib/* -maxdepth 0 ! \( -name 'snaps' -o -name 'seed' \) -exec cp -rf {} /var/lib/snapd \;
    sync_snaps_dir_state
    sync_seed_dir_state
}

clean_snaps_dir_state() {
    find /var/lib/snapd/snaps/* -maxdepth 0 ! -name '*.snap' -exec rm -rf {} +
}

clean_seed_dir_state() {
    find /var/lib/snapd/seed/* -maxdepth 0 ! -name 'snaps' -exec rm -rf {} +
    find /var/lib/snapd/seed/snaps/* -maxdepth 0 ! -name '*.snap' -exec rm -rf {} +
}

sync_snaps_dir_state() {
    find "$SNAPD_STATE_PATH"/snapd-lib/snaps/* -maxdepth 0 ! -name '*.snap' -exec cp -rf {} /var/lib/snapd/snaps \;
    sync_snaps "$SNAPD_STATE_PATH"/snapd-lib/snaps /var/lib/snapd/snaps
}

sync_seed_dir_state() {
    find "$SNAPD_STATE_PATH"/snapd-lib/seed/* -maxdepth 0 ! -name 'snaps' -exec cp -rf {} /var/lib/snapd/seed \;
    find "$SNAPD_STATE_PATH"/snapd-lib/seed/snaps/* -maxdepth 0 ! -name '*.snap' -exec cp -rf {} /var/lib/snapd/seed/snaps \;
    sync_snaps "$SNAPD_STATE_PATH"/snapd-lib/seed/snaps /var/lib/snapd/seed/snaps
}

sync_snaps() {
    local SOURCE=$1
    local TARGET=$2

    if ! [ -d "$SOURCE" ]; then
        rm -rf "$TARGET"
        return
    elif ! [ -d "$TARGET" ]; then
        mkdir -p "$TARGET"
    fi

    # Remove new snaps and changed ones
    for f in "$TARGET"/*.snap; do
        fname="$(basename $f)"
        if ! [ -e "$TARGET/$fname" ]; then
            break
        elif ! [ -e "$SOURCE/$fname" ]; then
            rm "$TARGET/$fname"
        elif [ "$TARGET/$fname" -nt "$SOURCE/$fname" ]; then
            cp -f "$SOURCE/$fname" "$TARGET/$fname"
        fi
    done

    # Add deleted snaps
    for f in "$SOURCE"/*.snap; do
        fname="$(basename $f)"
        if ! [ -e "$SOURCE/$fname" ]; then
            break
        elif ! [ -e "$TARGET/$fname" ]; then
            cp -f "$SOURCE/$fname" "$TARGET/$fname"
        fi
    done
}
