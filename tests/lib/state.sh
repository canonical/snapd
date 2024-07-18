#!/bin/bash

SNAPD_STATE_PATH="$TESTSTMP/snapd-state"
SNAPD_STATE_FILE="$TESTSTMP/snapd-state/snapd-state.tar"
SNAPD_ACTIVE_UNITS="$RUNTIME_STATE_PATH/snapd-active-units"

delete_snapd_state() {
    rm -rf "$SNAPD_STATE_PATH"
}

prepare_state() {
    mkdir -p "$SNAPD_STATE_PATH" "$RUNTIME_STATE_PATH"
}

is_snapd_state_saved() {
    if os.query is-core && [ -d "$SNAPD_STATE_PATH"/snapd-lib ]; then
        return 0
    elif os.query is-classic && [ -f "$SNAPD_STATE_FILE" ]; then
        return 0
    else
        return 1
    fi
}

get_active_snapd_units() {
    systemctl list-units --plain --state=active | grep -Eo '^snapd\..*(socket|service|timer)' || true
}

save_snapd_state() {
    prepare_state
    if os.query is-core; then
        boot_path="$("$TESTSTOOLS"/boot-state boot-path)"
        test -n "$boot_path" || return 1

        mkdir -p "$SNAPD_STATE_PATH"/system-units

        # Copy the state preserving the timestamps
        cp -a /var/lib/snapd "$SNAPD_STATE_PATH"/snapd-lib
        cp -rf /var/cache/snapd "$SNAPD_STATE_PATH"/snapd-cache
        cp -rf "$boot_path" "$SNAPD_STATE_PATH"/boot
        cp -f /etc/systemd/system/snap-*core*.mount "$SNAPD_STATE_PATH"/system-units
        mkdir -p "$SNAPD_STATE_PATH"/var-snap
        cp -a /var/snap/* "$SNAPD_STATE_PATH"/var-snap/
    else
        systemctl daemon-reload
        SNAP_MOUNT_DIR="$(os.paths snap-mount-dir)"
        escaped_snap_mount_dir="$(systemd-escape --path "$SNAP_MOUNT_DIR")"
        units="$(systemctl list-unit-files --full | grep -e "^${escaped_snap_mount_dir}[-.].*\\.mount" -e "^${escaped_snap_mount_dir}[-.].*\\.service" | cut -f1 -d ' ')"
        for unit in $units; do
            systemctl stop "$unit"
        done
        snapd_env="/etc/environment"
        snapd_service_env=$(ls -d /etc/systemd/system/snapd.*.d || true)
        snap_confine_profiles="$(ls /etc/apparmor.d/snap.core.* || true)"

        # shellcheck disable=SC2086
        tar cf "$SNAPD_STATE_FILE" \
            /var/lib/snapd \
            /var/cache/snapd \
            "$SNAP_MOUNT_DIR" \
            /etc/systemd/system/"$escaped_snap_mount_dir"-*core*.mount \
            /etc/systemd/system/snapd.mounts.target.wants/"$escaped_snap_mount_dir"-*core*.mount \
            /etc/systemd/system/multi-user.target.wants/"$escaped_snap_mount_dir"-*core*.mount \
            $snap_confine_profiles \
            $snapd_env \
            $snapd_service_env

        systemctl daemon-reload # Workaround for http://paste.ubuntu.com/17735820/
        core="$(readlink -f "$SNAP_MOUNT_DIR/core/current")"
        # on 14.04 it is possible that the core snap is still mounted at this point, unmount
        # to prevent errors starting the mount unit
        if os.query is-trusty && mount | grep -q "$core"; then
            umount "$core" || true
        fi
        for unit in $units; do
            systemctl start "$unit"
        done
    fi

    # Save the snapd active units
    get_active_snapd_units > "$SNAPD_ACTIVE_UNITS"
}

restore_snapd_state() {
    if os.query is-core; then
        # we need to ensure that we also restore the boot environment
        # fully for tests that break it
        boot_path="$("$TESTSTOOLS"/boot-state boot-path)"
        test -n "$boot_path" || return 1

        restore_snapd_lib
        cp -rf "$SNAPD_STATE_PATH"/snapd-cache/*  /var/cache/snapd
        cp -rf "$SNAPD_STATE_PATH"/boot/* "$boot_path"
        cp -f "$SNAPD_STATE_PATH"/system-units/*  /etc/systemd/system
        rm -rf /var/snap/*
        cp -a "$SNAPD_STATE_PATH"/var-snap/* /var/snap/
    else
        # Purge all the systemd service units config
        rm -rf /etc/systemd/system/snapd.service.d
        rm -rf /etc/systemd/system/snapd.socket.d

        # TODO: remove files created by the test
        tar -C/ -xf "$SNAPD_STATE_FILE"
    fi

    # Start all the units which have to be active
    while read -r unit; do
        if ! systemctl is-active "$unit"; then
            systemctl start "$unit"
        fi
    done  < "$SNAPD_ACTIVE_UNITS"
}

restore_snapd_lib() {
    # Clean all the state but the snaps, seed, cache and kernel dirs. Then make
    # a selective clean for snaps, seed and cache dirs leaving the .snap files
    # which then are going to be synchronized. We cannot touch kernel dir as it
    # is bind mounted in /lib/{modules,firmware}.
    find /var/lib/snapd/* -maxdepth 0 ! \( -name 'snaps' -o -name 'seed' -o -name 'cache' -o -name 'kernel' \) -exec rm -rf {} \;

    # Copy the whole state but the snaps, seed, cache and kernel dirs
    find "$SNAPD_STATE_PATH"/snapd-lib/* -maxdepth 0 ! \( -name 'snaps' -o -name 'seed' -o -name 'cache' -o -name 'kernel' \) -exec cp -rf {} /var/lib/snapd \;

    # Synchronize snaps, seed and cache directories. The this is done separately in order to avoid copying
    # the snap files due to it is a heavy task and take most of the time of the restore phase.
    rsync -av --delete "$SNAPD_STATE_PATH"/snapd-lib/snaps /var/lib/snapd
    if os.query is-core16 || os.query is-core18; then
        rsync -av --delete "$SNAPD_STATE_PATH"/snapd-lib/seed/ /var/lib/snapd/seed/
    else
        # TODO:UC20: /var/lib/snapd/seed is a read only bind mount, use the rw
        # mount or later mount seed as needed
        rsync -av --delete "$SNAPD_STATE_PATH"/snapd-lib/seed/ /run/mnt/ubuntu-seed/
    fi
    rsync -av --delete "$SNAPD_STATE_PATH"/snapd-lib/cache /var/lib/snapd
}

remove_disabled_snaps() {
    snap list --all | grep disabled | while read -r name _ revision _ ; do
        snap remove "$name" --revision="$revision"
    done
}
