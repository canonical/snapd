#!/bin/sh -e

# Create the directory defined in the 'slots' section of snap.yaml
# source: write: [$SNAP_DATA/data]
mkdir -p "$SNAP_DATA/data"

# Write a unique string to a file to verify identity later
echo "ConnectionActive" > "$SNAP_DATA/data/marker.txt"

echo "Provider: Data generated at $SNAP_DATA/data/marker.txt"