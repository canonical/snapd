#!/bin/sh -e

# Define the expected location based on the 'plugs' section of snap.yaml
# target: $SNAP_DATA/target
TARGET_FILE="$SNAP_DATA/target/marker.txt"

# Verify the file exists and contains the expected data
if [ -f "$TARGET_FILE" ]; then
    CONTENT=$(cat "$TARGET_FILE")
    if [ "$CONTENT" = "ConnectionActive" ]; then
        echo "Consumer: SUCCESS - Connection verification passed."
        exit 0
    else
        echo "Consumer: FAILURE - File found but content mismatch."
        exit 1
    fi
else
    echo "Consumer: FAILURE - Shared file not found at $TARGET_FILE."
    exit 1
fi