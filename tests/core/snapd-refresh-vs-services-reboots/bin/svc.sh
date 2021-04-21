#!/bin/bash -ex


# if the file exists, then we don't fork, we just sleep forever
if [ -f "$SNAP_DATA/prevent-start" ]; then
    rm -rf "$SNAP_DATA/prevent-start"
    sleep infinity
fi

# otherwise create the file and fork a process and then exit
sleep infinity &
touch "$SNAP_DATA/prevent-start"

exit 0