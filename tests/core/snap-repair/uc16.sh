#!/bin/bash -ex

# show what mode we are in
echo "$SNAP_SYSTEM_MODE"

# mark us as done using the snap-repair command manually
/usr/lib/snapd/snap-repair "done"
