#!/bin/bash

set -eu

badMATCH=$(find tests -name 'task.yaml' -print0 -o -name 'spread.yaml' -print0 |
        xargs -0 grep -R -n -E 'MATCH +-v' || true)
if [ -n "$badMATCH" ]; then
    echo "Potentially incorrect use of MATCH -v at the following locations:" >&2
    echo "$badMATCH" >&2
    exit 1
fi
