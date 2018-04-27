#!/bin/sh

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

    # Copy the state preserving the timestamps
    cp -rfp /var/lib/snapd "$SNAPD_STATE_PATH/snapdlib"
    cp -rf "$BOOT" "$SNAPD_STATE_PATH/boot"
    cp -f /etc/systemd/system/snap-*core*.mount "$SNAPD_STATE_PATH"
}

restore_all_snap_state() {
    # we need to ensure that we also restore the boot environment
    # fully for tests that break it
    get_boot_path

    mkdir -p /var/lib/snapd/snaps
    mkdir -p /var/lib/snapd/seed /var/lib/snapd/seed/snaps

    # Clean all the state but the snaps and seed dirs
    find /var/lib/snapd/* -maxdepth 0 ! \( -name 'snaps' -o -name 'seed' \) -exec rm -rf {} +
    clean_snaps_dir_state
    clean_seed_dir_state

    # Copy the whole state but the snaps and seed dirs
    find "$SNAPD_STATE_PATH"/snapdlib/* -maxdepth 0 ! \( -name 'snaps' -o -name 'seed' \) -exec cp -rf {} /var/lib/snapd \;
    sync_snaps_dir_state
    sync_seed_dir_state

    # Restore boot and mount points
    cp -rf "$SNAPD_STATE_PATH"/boot/* "$BOOT"
    cp -f "$SNAPD_STATE_PATH"/snap-*core*.mount  /etc/systemd/system
}

clean_snaps_dir_state() {
    find /var/lib/snapd/snaps/* -maxdepth 0 ! -name '*.snap' -exec rm -rf {} +
}

clean_seed_dir_state() {
    find /var/lib/snapd/seed/* -maxdepth 0 ! -name 'snaps' -exec rm -rf {} +
    find /var/lib/snapd/seed/snaps/* -maxdepth 0 ! -name '*.snap' -exec rm -rf {} +
}

sync_snaps_dir_state() {
    find "$SNAPD_STATE_PATH"/snapdlib/snaps/* -maxdepth 0 ! -name '*.snap' -exec cp -rf {} /var/lib/snapd/snaps \;
    sync_snaps "$SNAPD_STATE_PATH"/snapdlib/snaps /var/lib/snapd/snaps
}

sync_seed_dir_state() {
    find "$SNAPD_STATE_PATH"/snapdlib/seed/* -maxdepth 0 ! -name 'snaps' -exec cp -rf {} /var/lib/snapd/seed \;
    find "$SNAPD_STATE_PATH"/snapdlib/seed/snaps/* -maxdepth 0 ! -name '*.snap' -exec cp -rf {} /var/lib/snapd/seed/snaps \;
    sync_snaps "$SNAPD_STATE_PATH"/snapdlib/seed/snaps /var/lib/snapd/seed/snaps
}

sync_snaps() {
    SOURCE=$1
    TARGET=$2

    if ! [ -d "$SOURCE" ]; then
        echo "Source directory does not exist $SOURCE"
        exit 1
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
