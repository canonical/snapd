#!/bin/sh -e

rm -f "$SNAP_DATA/stamp"

while true; do
    if [ -e "$SNAP_DATA/stamp" ]; then
        echo "$SNAP_DATA/stamp found, exiting"
        break
    fi
    sleep 1
done &
