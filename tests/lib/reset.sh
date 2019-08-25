#!/bin/bash

set -e -x

# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"
# shellcheck source=tests/lib/state.sh
. "$TESTSLIB/state.sh"

# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB/systemd.sh"

#shellcheck source=tests/lib/systems.sh
. "$TESTSLIB"/systems.sh

reset_classic() {
    # Reload all service units as in some situations the unit might
    # have changed on the disk.
    systemctl daemon-reload
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

    case "$SPREAD_SYSTEM" in
        fedora-*|centos-*)
            # On systems running SELinux we need to restore the context of the
            # directories we just recreated. Otherwise, the entries created
            # inside will be incorrectly labeled.
            restorecon -F -v -R "$SNAP_MOUNT_DIR" /var/snap /var/lib/snapd
            ;;
    esac

    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        systemctl start snap.mount.service
    fi

    rm -rf /root/.snap/gnupg
    rm -f /tmp/core* /tmp/ubuntu-core*

    if [ "$1" = "--reuse-core" ]; then
        # Restore snapd state and start systemd service units
        restore_snapd_state
        escaped_snap_mount_dir="$(systemd-escape --path "$SNAP_MOUNT_DIR")"
        mounts="$(systemctl list-unit-files --full | grep "^${escaped_snap_mount_dir}[-.].*\\.mount" | cut -f1 -d ' ')"
        services="$(systemctl list-unit-files --full | grep "^${escaped_snap_mount_dir}[-.].*\\.service" | cut -f1 -d ' ')"
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

reset_all_snap() {
    # remove all leftover snaps
    # shellcheck source=tests/lib/names.sh
    . "$TESTSLIB/names.sh"

    remove_bases=""
    # remove all app snaps first
    for snap in "$SNAP_MOUNT_DIR"/*; do
        snap="${snap:6}"
        case "$snap" in
            "bin" | "$gadget_name" | "$kernel_name" | "$core_name" | "snapd" |README)
                ;;
            *)
                # make sure snapd is running before we attempt to remove snaps, in case a test stopped it
                if ! systemctl status snapd.service snapd.socket >/dev/null; then
                    systemctl start snapd.service snapd.socket
                fi
                if ! echo "$SKIP_REMOVE_SNAPS" | grep -w "$snap"; then
                    if snap info --verbose "$snap" | grep -E '^type: +(base|core)'; then
                        if [ -z "$remove_bases" ]; then
                            remove_bases="$snap"
                        else
                            remove_bases="$remove_bases $snap"
                        fi
                    else
                        snap remove "$snap"
                    fi
                fi
                ;;
        esac
    done
    # remove all base/os snaps at the end
    if [ -n "$remove_bases" ]; then
        for base in $remove_bases; do
            snap remove "$base"
            if [ -d "$SNAP_MOUNT_DIR/$base" ]; then
                echo "Error: removing base $base has unexpected leftover dir $SNAP_MOUNT_DIR/$base"
                ls -al "$SNAP_MOUNT_DIR"
                ls -al "$SNAP_MOUNT_DIR/$base"
                exit 1
            fi
        done
    fi

    # ensure we have the same state as initially
    systemctl stop snapd.service snapd.socket
    restore_snapd_state
    rm -rf /root/.snap
    if [ "$1" != "--keep-stopped" ]; then
        systemctl start snapd.service snapd.socket
    fi

    # Exit in case there is a snap in broken state after restoring the snapd state
    if snap list | grep -E "broken$"; then
        echo "snap in broken state"
        exit 1
    fi

}

# When the variable REUSE_SNAPD is set to 1, we don't remove and purge snapd.
# In that case we just cleanup the environment by removing installed snaps as
# it is done for core systems.
if is_core_system || [ "$REUSE_SNAPD" = 1 ]; then
    reset_all_snap "$@"
else
    reset_classic "$@"
fi

# Discard all mount namespaces and active mount profiles.
# This is duplicating logic in snap-discard-ns but it doesn't
# support --all switch yet so we cannot use it.
if [ -d /run/snapd/ns ]; then
    for mnt in /run/snapd/ns/*.mnt; do
        umount -l "$mnt" || true
        rm -f "$mnt"
    done
    find /run/snapd/ns/ \( -name '*.fstab' -o -name '*.user-fstab' -o -name '*.info' \) -delete
fi

if [ "$REMOTE_STORE" = staging ] && [ "$1" = "--store" ]; then
    # shellcheck source=tests/lib/store.sh
    . "$TESTSLIB"/store.sh
    teardown_staging_store
fi
