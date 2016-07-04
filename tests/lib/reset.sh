#!/bin/bash

set -e -x

# purge all state
$PROJECT_PATH/debian/snapd.postrm purge

if [ "$1" = "--reuse-core" ]; then 
	$(cd / && tar xzf $SPREAD_PATH/snapd-state.tar.gz)
	mounts="$(systemctl list-unit-files | grep '^snap[-.].*\.mount' | cut -f1 -d ' ')"
	services="$(systemctl list-unit-files | grep '^snap[-.].*\.service' | cut -f1 -d ' ')"
        systemctl daemon-reload # Workaround for http://paste.ubuntu.com/17735820/
	for unit in $mounts $services; do
	    systemctl start $unit
	done
fi
systemctl start snapd
