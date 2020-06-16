#!/bin/bash -e

echo "snap-svc-did-this" > "$SNAP_DATA/data"

exec sleep infinity
