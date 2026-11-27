#!/bin/bash

set -eu

badmultiline=$(find tests -name 'task.yaml' -print0 -o -name 'spread.yaml' -print0 |
        xargs -0 grep -R -n -E '(restore*|prepare*|execute|debug):\s*$' || true)
if [ -n "$badmultiline" ]; then
    echo "Incorrect multiline strings at the following locations:" >&2
    echo "$badmultiline" >&2
    exit 1
fi
