#!/bin/bash

set -e -x

reset_classic() {
    systemctl stop snapd.service snapd.socket

    # purge all state
    sh -x ${SPREAD_PATH}/debian/snapd.postrm purge
    mkdir -p /snap /var/snap /var/lib/snapd
    if [ -d /snap/* ] || [ -d /var/snap/* ]; then
        echo "postinst purge failed"
        ls -lR /snap/* /var/snap/*
        exit 1
    fi

    rm -f /tmp/ubuntu-core*

    if [ "$1" = "--reuse-core" ]; then
        $(cd / && tar xzf $SPREAD_PATH/snapd-state.tar.gz)
	mounts="$(systemctl list-unit-files | grep '^snap[-.].*\.mount' | cut -f1 -d ' ')"
	services="$(systemctl list-unit-files | grep '^snap[-.].*\.service' | cut -f1 -d ' ')"
        systemctl daemon-reload # Workaround for http://paste.ubuntu.com/17735820/
	for unit in $mounts $services; do
	    systemctl start $unit
	done
    fi
    systemctl start snapd.socket

    # wait for snapd listening
    while ! printf "GET / HTTP/1.0\r\n\r\n" | nc -U -q 1 /run/snapd.socket; do sleep 0.5; done
}

reset_all_snap() {
    # remove all leftover snaps
    . $TESTSLIB/gadget.sh
    gadget_name=$(get_gadget_name)
    for snap in $(ls /snap); do
        if [ "$snap" = "bin" ] || [ "$snap" = "$gadget_name" ] ||  [ "$snap" = "${gadget_name}-kernel" ] || [ "$snap" = "ubuntu-core" ]; then
            continue
        fi
        snap remove $snap
    done

    # ensure we have the same state as initially
    systemctl stop snapd.service snapd.socket
    $(cd / && tar xzf $SPREAD_PATH/snapd-state.tar.gz)
    rm -rf /root/.snap
    systemctl start snapd.service snapd.socket
}

if [ "$SPREAD_SYSTEM" = "ubuntu-core-16-64" ]; then
    reset_all_snap "$@"
else
    reset_classic "$@"
fi
