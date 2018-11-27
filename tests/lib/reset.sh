#!/bin/bash

# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"

# shellcheck source=tests/lib/state.sh
. "$TESTSLIB/state.sh"

# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB/systemd.sh"

#shellcheck source=tests/lib/systems.sh
. "$TESTSLIB/systems.sh"

init_state_classic() {
    if [ "$1" = "--reuse-core" ]; then
        # Restore snapd state and start systemd service units
        restore_snapd_state
        escaped_snap_mount_dir="$(systemd-escape --path "$SNAP_MOUNT_DIR")"
        all_units="$(systemctl list-unit-files --full | cut -f1 -d ' ')"
        mounts=""
        if echo "$all_units" | grep "^${escaped_snap_mount_dir}[-.].*\\.mount"; then
            mounts="$(echo "$all_units" | grep "^${escaped_snap_mount_dir}[-.].*\\.mount")"
        fi
        services=""
        if echo "$all_units" | grep "^${escaped_snap_mount_dir}[-.].*\\.service"; then
            services="$(echo "$all_units" | grep "^${escaped_snap_mount_dir}[-.].*\\.service")"
        fi
        systemctl daemon-reload # Workaround for http://paste.ubuntu.com/17735820/
        for unit in $mounts $services; do
            systemctl start "$unit"
        done

        # force all profiles to be re-generated
        rm -f /var/lib/snapd/system-key
    fi

    if [ "$1" != "--keep-stopped" ]; then
        systemctl start snapd.socket

        # wait for snapd listening
        EXTRA_NC_ARGS="-q 1"
        case "$SPREAD_SYSTEM" in
            fedora-*|amazon-*|centos-*)
                EXTRA_NC_ARGS=""
                ;;
        esac
        while ! printf 'GET / HTTP/1.0\r\n\r\n' | nc -U $EXTRA_NC_ARGS /run/snapd.socket; do sleep 0.5; done
    fi
}

clean_classic() {
    # Reload all service units as in some situations the unit might
    # have changed on the disk.
    systemctl daemon-reload

    echo "Ensure the service is active before stopping it"
    retries=20
    systemctl status snapd.service snapd.socket || true
    while systemctl status snapd.service snapd.socket | grep "Active: activating"; do
        if [ $retries -eq 0 ]; then
            echo "snapd service or socket not active"
            exit 1
        fi
        retries=$(( retries - 1 ))
        sleep 1
    done

    systemd_stop_units snapd.service snapd.socket

    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            sh -x "${SPREAD_PATH}/debian/snapd.postrm" purge
            ;;
        fedora-*|opensuse-*|arch-*|amazon-*|centos-*)
            # We don't know if snap-mgmt was built, so call the *.in file
            # directly and pass arguments that will override the placeholders
            sh -x "${SPREAD_PATH}/cmd/snap-mgmt/snap-mgmt.sh.in" \
                --snap-mount-dir="$SNAP_MOUNT_DIR" \
                --purge
            # The script above doesn't remove the snapd directory as this
            # is normally done by the rpm packaging system.
            rm -rf /var/lib/snapd
            ;;
        *)
            echo "don't know how to reset $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
    # extra purge
    rm -rvf /var/snap "${SNAP_MOUNT_DIR:?}/bin"
    mkdir -p "$SNAP_MOUNT_DIR" /var/snap /var/lib/snapd
    if [ "$(find "$SNAP_MOUNT_DIR" /var/snap -mindepth 1 -print -quit)" ]; then
        echo "postinst purge failed"
        ls -lR "$SNAP_MOUNT_DIR"/ /var/snap/
        exit 1
    fi

    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        systemctl start snap.mount.service
    fi

    rm -rf /root/.snap/gnupg
    rm -f /tmp/core* /tmp/ubuntu-core*
}

init_state_all_snap() {
    # ensure we have the same state as initially
    systemctl stop snapd.service snapd.socket
    restore_snapd_state
    rm -rf /root/.snap
    if [ "$1" != "--keep-stopped" ]; then
        systemctl start snapd.service snapd.socket
    fi
}

clean_all_snap() {
    # remove all leftover snaps
    # shellcheck source=tests/lib/names.sh
    . "$TESTSLIB/names.sh"

    remove_bases=""
    # remove all app snaps first
    for snap in "$SNAP_MOUNT_DIR"/*; do
        snap="${snap:6}"
        case "$snap" in
            "bin" | "$gadget_name" | "$kernel_name" | "$core_name" | "core" | README)
                ;;
            *)
                # make sure snapd is running before we attempt to remove snaps, in case a test stopped it
                if ! systemctl status snapd.service snapd.socket; then
                    systemctl start snapd.service snapd.socket
                fi
                if ! echo "$SKIP_REMOVE_SNAPS" | grep -w "$snap"; then
                    if snap info "$snap" | grep -E '^type: +(base|core)'; then
                        remove_bases="$remove_bases $snap"
                    else
                        snap remove "$snap"
                    fi
                fi
                ;;
        esac
    done
    # remove all base/os snaps at the end
    if [ -n "$remove_bases" ]; then
        snap remove "$remove_bases"
    fi
}

discard_ns() {
    # Discard all mount namespaces and active mount profiles.
    # This is duplicating logic in snap-discard-ns but it doesn't
    # support --all switch yet so we cannot use it.
    if [ -d /run/snapd/ns ]; then
        for mnt in /run/snapd/ns/*.mnt; do
            umount -l "$mnt" || true
            rm -f "$mnt"
        done
        rm -f /run/snapd/ns/*.fstab
    fi
}

tear_down_store() {
    if [ "$REMOTE_STORE" = staging ] && [ "$1" = "--store" ]; then
        # shellcheck source=tests/lib/store.sh
        . "$TESTSLIB"/store.sh
        teardown_staging_store
    fi  
}

reset_snapd() {
    if is_core_system; then
        clean_all_snap "$@"
        init_state_all_snap "$@"
        discard_ns
        tear_down_store "$@"
    else
        clean_classic "$@"
        init_state_classic "$@"
        discard_ns
        tear_down_store "$@"
    fi    
}


