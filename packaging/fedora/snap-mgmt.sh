#!/bin/bash

# Overlord management of snapd for package manager actions.
# Implements actions that would be invoked in %pre(un) actions for snapd.
# Derived from the snapd.postrm scriptlet used in the Ubuntu packaging for
# snapd.

set -e

SNAP_MOUNT_DIR="/var/lib/snapd/snap"

show_help() {
    exec cat <<'EOF'
Usage: snap-mgmt.sh [OPTIONS]

A simple script to cleanup snap installations.

optional arguments:
  --help                           Show this help message and exit
  --snap-mount-dir=<path>          Provide a path to be used as $SNAP_MOUNT_DIR
  --purge                          Purge all data from $SNAP_MOUNT_DIR
EOF
}

SNAP_UNIT_PREFIX="$(systemd-escape -p ${SNAP_MOUNT_DIR})"

systemctl_stop() {
    unit="$1"
    if systemctl is-active -q "$unit"; then
        echo "Stoping $unit"
        systemctl stop -q "$unit" || true
    fi
}

purge() {
    # undo any bind mount to ${SNAP_MOUNT_DIR} that resulted from LP:#1668659
    if grep -q "${SNAP_MOUNT_DIR} ${SNAP_MOUNT_DIR}" /proc/self/mountinfo; then
        umount -l "${SNAP_MOUNT_DIR}" || true
    fi

    mounts=$(systemctl list-unit-files --full | grep "^${SNAP_UNIT_PREFIX}[-.].*\.mount" | cut -f1 -d ' ')
    services=$(systemctl list-unit-files --full | grep "^${SNAP_UNIT_PREFIX}[-.].*\.service" | cut -f1 -d ' ')
    for unit in $services $mounts; do
        # ensure its really a snap mount unit or systemd unit
        if ! grep -q 'What=/var/lib/snapd/snaps/' "/etc/systemd/system/$unit" && ! grep -q 'X-Snappy=yes' "/etc/systemd/system/$unit"; then
            echo "Skipping non-snapd systemd unit $unit"
            continue
        fi

        echo "Stopping $unit"
        systemctl_stop "$unit"

        # if it is a mount unit, we can find the snap name in the mount
        # unit (we just ignore unit files)
        snap=$(grep "Where=${SNAP_MOUNT_DIR}/" "/etc/systemd/system/$unit"|cut -f3 -d/)
        rev=$(grep "Where=${SNAP_MOUNT_DIR}/" "/etc/systemd/system/$unit"|cut -f4 -d/)
        if [ -n "$snap" ]; then
            echo "Removing snap $snap"
            # aliases
            if [ -d "${SNAP_MOUNT_DIR}/bin" ]; then
                find "${SNAP_MOUNT_DIR}/bin" -maxdepth 1 -lname "$snap" -delete
                find "${SNAP_MOUNT_DIR}/bin" -maxdepth 1 -lname "$snap.*" -delete
            fi
            # generated binaries
            rm -f "${SNAP_MOUNT_DIR}/bin/$snap"
            rm -f "${SNAP_MOUNT_DIR}/bin/$snap".*
            # snap mount dir
            umount -l "${SNAP_MOUNT_DIR}/$snap/$rev" 2> /dev/null || true
            rm -rf "${SNAP_MOUNT_DIR:?}/$snap/$rev"
            rm -f "${SNAP_MOUNT_DIR}/$snap/current"
            # snap data dir
            rm -rf "/var/snap/$snap/$rev"
            rm -rf "/var/snap/$snap/common"
            rm -f "/var/snap/$snap/current"
            # opportunistic remove (may fail if there are still revisions left)
            for d in "${SNAP_MOUNT_DIR}/$snap" "/var/snap/$snap"; do
                if [ -d "$d" ]; then
                    rmdir --ignore-fail-on-non-empty "$d"
                fi
            done
        fi

        echo "Removing $unit"
        rm -f "/etc/systemd/system/$unit"
        rm -f "/etc/systemd/system/multi-user.target.wants/$unit"
    done

    echo "Discarding preserved snap namespaces"
    # opportunistic as those might not be actually mounted
    for mnt in /run/snapd/ns/*.mnt; do
        umount -l "$mnt" || true
    done
    for fstab in /run/snapd/ns/*.fstab; do
        rm -f "$fstab"
    done
    umount -l /run/snapd/ns/ || true


    echo "Removing downloaded snaps"
    rm -rf /var/lib/snapd/snaps/*

    echo "Final directory cleanup"
    rm -rf "${SNAP_MOUNT_DIR}"
    rm -rf /var/snap

    echo "Removing leftover snap shared state data"
    rm -rf /var/lib/snapd/desktop/applications/*
    rm -rf /var/lib/snapd/seccomp/bpf/*
    rm -rf /var/lib/snapd/device/*
    rm -rf /var/lib/snapd/assertions/*

    echo "Removing snapd catalog cache"
    rm -f /var/cache/snapd/*
}

while [ -n "$1" ]; do
    case "$1" in
        --help)
            show_help
            exit
            ;;
        --snap-mount-dir=*)
            SNAP_MOUNT_DIR=${1#*=}
            SNAP_UNIT_PREFIX=$(systemd-escape -p "$SNAP_MOUNT_DIR")
            shift
            ;;
        --purge)
            purge
            shift
            ;;
        *)
            echo "Unknown command: $1"
            exit 1
            ;;
    esac
done
