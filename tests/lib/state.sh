#!/bin/bash

SNAPD_STATE_PATH="$GOPATH/snapd-state"

# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"

# shellcheck source=tests/lib/boot.sh
. "$TESTSLIB/boot.sh"

delete_snapd_state() {
    rm -rf $SNAPD_STATE_PATH
}

save_classic_state() {
    local escaped_snap_mount_dir=$1

    mkdir -p "$SNAPD_STATE_PATH" "$SNAPD_STATE_PATH"/snap-confine-profiles "$SNAPD_STATE_PATH"/system-units "$SNAPD_STATE_PATH"/multi-user-units

    cp -rfp /var/lib/snapd "$SNAPD_STATE_PATH"/snapd-lib
    cp -rf "$SNAP_MOUNT_DIR" "$SNAPD_STATE_PATH"/snap-mount-dir
    cp -f /etc/systemd/system/"$escaped_snap_mount_dir"-*core*.mount "$SNAPD_STATE_PATH"/system-units
    cp -f /etc/systemd/system/multi-user.target.wants/"$escaped_snap_mount_dir"-*core*.mount "$SNAPD_STATE_PATH"/multi-user-units
    cp -f /etc/environment "$SNAPD_STATE_PATH"/environment
    cp -rf /etc/systemd/system/snapd.service.d "$SNAPD_STATE_PATH"/snap.service.d
    cp -rf /etc/systemd/system/snapd.socket.d "$SNAPD_STATE_PATH"/snap.socket.d
    cp -rf /etc/apparmor.d/snap.core.* "$SNAPD_STATE_PATH"/snap-confine-profiles
}

restore_classic_state() {
    # Purge all the systemd service units config
    rm -rf /etc/systemd/system/snapd.service.d
    rm -rf /etc/systemd/system/snapd.socket.d

    restore_snapd_lib
    cp -rf "$SNAPD_STATE_PATH"/snap-mount-dir/* "$SNAP_MOUNT_DIR"
    cp -f "$SNAPD_STATE_PATH"/system-units/* /etc/systemd/system
    cp -f "$SNAPD_STATE_PATH"/multi-user-units/* /etc/systemd/system/multi-user.target.wants/
    cp -f "$SNAPD_STATE_PATH"/environment /etc/environment
    cp -rf "$SNAPD_STATE_PATH"/snap.service.d /etc/systemd/system/snapd.service.d
    cp -rf "$SNAPD_STATE_PATH"/snap.socket.d /etc/systemd/system/snapd.socket.d
    cp -rf "$SNAPD_STATE_PATH"/snap-confine-profiles/* /etc/apparmor.d
}

save_all_snap_state() {
    local boot_path="$(get_boot_path)"

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
    if [ ! -z "$(ls -A /var/lib/snapd/snaps)" ]; then
        find /var/lib/snapd/snaps/* -maxdepth 0 ! -name '*.snap' -exec rm -rf {} +
    fi
}

clean_seed_dir_state() {
    find /var/lib/snapd/seed/* -maxdepth 0 ! -name 'snaps' -exec rm -rf {} +
    if [ ! -z "$(ls -A /var/lib/snapd/seed/snaps)" ]; then
        find /var/lib/snapd/seed/snaps/* -maxdepth 0 ! -name '*.snap' -exec rm -rf {} +
    fi
}

sync_snaps_dir_state() {
    if [ ! -z "$(ls -A "$SNAPD_STATE_PATH"/snapd-lib/snaps)" ]; then
        find "$SNAPD_STATE_PATH"/snapd-lib/snaps/* -maxdepth 0 ! -name '*.snap' -exec cp -rf {} /var/lib/snapd/snaps \;
    fi
    sync_snaps "$SNAPD_STATE_PATH"/snapd-lib/snaps /var/lib/snapd/snaps
}

sync_seed_dir_state() {
    if [ ! -z "$(ls -A "$SNAPD_STATE_PATH"/snapd-lib/seed)" ]; then
        find "$SNAPD_STATE_PATH"/snapd-lib/seed/* -maxdepth 0 ! -name 'snaps' -exec cp -rf {} /var/lib/snapd/seed \;
    fi
    if [ ! -z "$(ls -A "$SNAPD_STATE_PATH"/snapd-lib/seed/snaps)" ]; then
        find "$SNAPD_STATE_PATH"/snapd-lib/seed/snaps/* -maxdepth 0 ! -name '*.snap' -exec cp -rf {} /var/lib/snapd/seed/snaps \;
    fi
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
