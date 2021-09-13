#!/bin/bash -ex

# show what mode we are in
echo "$SNAP_SYSTEM_MODE"

# mark us as done using the repair helper command
repair "done"
