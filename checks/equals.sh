#!/bin/bash

set -eu

if got=$(grep -n -R -E "(\!=|==|Equals,) (state\.)?ErrNoState" --include=*.go); then
    echo "Don't use equality checks with ErrNoState, use errors.Is() instead" >&2
    echo "$got" >&2
    exit 1
fi
