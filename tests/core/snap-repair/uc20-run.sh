#!/bin/bash -ex

# show what mode we are in
echo "$SNAP_SYSTEM_MODE"

# mark us as done using the env var
echo "done" >&"$SNAP_REPAIR_STATUS_FD"
