#!/bin/bash -ex

# show what mode we are in
echo "$SNAP_SYSTEM_MODE"

# touch a file in /host to show that we did something from recover mode that 
# persists to run mode
touch /host/ubuntu-data/system-data/var/lib/snapd/FIXED

# mark us as done using the snap-repair command
/usr/lib/snapd/snap-repair "done"
