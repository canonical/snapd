#!/bin/bash

set -e -x

reset_classic() {
    systemctl stop snapd.service snapd.socket

    # purge all state
    sh -x ${SPREAD_PATH}/debian/snapd.postrm purge
    # extra purge
    rm -rvf /var/snap /snap/bin
    mkdir -p /snap /var/snap /var/lib/snapd
    if [ "$(find /snap /var/snap -mindepth 1 -print -quit)" ]; then
        echo "postinst purge failed"
        ls -lR /snap/ /var/snap/
        exit 1
    fi

    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        systemctl start snap.mount.service
    fi

    rm -rf /root/.snap/gnupg
    rm -f /tmp/core* /tmp/ubuntu-core*

    if [ "$1" = "--reuse-core" ]; then
        $(cd / && tar xzf $SPREAD_PATH/snapd-state.tar.gz)
        mounts="$(systemctl list-unit-files --full | grep '^snap[-.].*\.mount' | cut -f1 -d ' ')"
        services="$(systemctl list-unit-files --full | grep '^snap[-.].*\.service' | cut -f1 -d ' ')"
        systemctl daemon-reload # Workaround for http://paste.ubuntu.com/17735820/
        for unit in $mounts $services; do
            systemctl start $unit
        done
    fi

    if [ "$1" != "--keep-stopped" ]; then
        systemctl start snapd.socket

        # wait for snapd listening
        while ! printf "GET / HTTP/1.0\r\n\r\n" | nc -U -q 1 /run/snapd.socket; do sleep 0.5; done
    fi
}

reset_all_snap() {
    # remove all leftover snaps
    . "$TESTSLIB/names.sh"

    for snap in /snap/*; do
        snap="${snap:6}"
        case "$snap" in
            "bin" | "$gadget_name" | "$kernel_name" | "$core_name" )
                ;;
            *)
                snap remove "$snap"
                ;;
        esac
    done

    # ensure we have the same state as initially
    systemctl stop snapd.service snapd.socket
    rm -rf /var/lib/snapd/*
    $(cd / && tar xzf $SPREAD_PATH/snapd-state.tar.gz)
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
    . $TESTSLIB/store.sh
    teardown_staging_store
fi
