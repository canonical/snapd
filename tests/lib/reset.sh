#!/bin/bash

set -e -x

systemctl stop snapd.service snapd.socket

# purge all state
$PROJECT_PATH/debian/snapd.postrm purge

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
