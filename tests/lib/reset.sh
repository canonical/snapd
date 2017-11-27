#!/bin/bash

set -e -x

# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"

reset_classic() {
    # Reload all service units as in some situations the unit might
    # have changed on the disk.
    systemctl daemon-reload

    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        echo "Ensure the service is active before stop it"
        retries=20
        systemctl status snapd.service || true
        while systemctl status snapd.service | grep "Active: activating"; do
            if [ $retries -eq 0 ]; then
                echo "snapd service not active"
                exit 1
            fi
            retries=$(( $retries - 1 ))
            sleep 1
        done
    fi

    systemctl stop snapd.service snapd.socket

    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            sh -x "${SPREAD_PATH}/debian/snapd.postrm" purge
            ;;
        fedora-*|opensuse-*)
            sh -x "${SPREAD_PATH}/packaging/fedora/snap-mgmt.sh" \
                --snap-mount-dir="$SNAP_MOUNT_DIR" \
                --purge
            # The script above doesn't remove the snapd directory as this
            # is normally done by the rpm packaging system.
            rm -rf /var/lib/snapd
            ;;
        *)
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

    if [ "$1" = "--reuse-core" ]; then
        # Purge all the systemd service units config
        rm -rf /etc/systemd/system/snapd.service.d
        rm -rf /etc/systemd/system/snapd.socket.d

        # Restore snapd state and start systemd service units
        tar -C/ -xzf "$SPREAD_PATH/snapd-state.tar.gz"
        escaped_snap_mount_dir="$(systemd-escape --path "$SNAP_MOUNT_DIR")"
        mounts="$(systemctl list-unit-files --full | grep "^$escaped_snap_mount_dir[-.].*\.mount" | cut -f1 -d ' ')"
        services="$(systemctl list-unit-files --full | grep "^$escaped_snap_mount_dir[-.].*\.service" | cut -f1 -d ' ')"
        systemctl daemon-reload # Workaround for http://paste.ubuntu.com/17735820/
        for unit in $mounts $services; do
            systemctl start "$unit"
        done
    fi

    if [ "$1" != "--keep-stopped" ]; then
        systemctl start snapd.socket

        # wait for snapd listening
        EXTRA_NC_ARGS="-q 1"
        if [[ "$SPREAD_SYSTEM" = fedora-* ]]; then
            EXTRA_NC_ARGS=""
        fi
        while ! printf "GET / HTTP/1.0\r\n\r\n" | nc -U $EXTRA_NC_ARGS /run/snapd.socket; do sleep 0.5; done
    fi
}

reset_all_snap() {
    # remove all leftover snaps
    # shellcheck source=tests/lib/names.sh
    . "$TESTSLIB/names.sh"

    for snap in "$SNAP_MOUNT_DIR"/*; do
        snap="${snap:6}"
        case "$snap" in
            "bin" | "$gadget_name" | "$kernel_name" | core | README)
                ;;
            *)
                snap remove "$snap"
                ;;
        esac
    done

    # ensure we have the same state as initially
    systemctl stop snapd.service snapd.socket
    rm -rf /var/lib/snapd/*
    tar -C/ -xzf "$SPREAD_PATH/snapd-state.tar.gz"
    rm -rf /root/.snap
    if [ "$1" != "--keep-stopped" ]; then
        systemctl start snapd.service snapd.socket
    fi
}

if [[ "$SPREAD_SYSTEM" == ubuntu-core-16-* ]]; then
    reset_all_snap "$@"
else
    reset_classic "$@"
fi

if [ "$REMOTE_STORE" = staging ] && [ "$1" = "--store" ]; then
    # shellcheck source=tests/lib/store.sh
    . $TESTSLIB/store.sh
    teardown_staging_store
fi
