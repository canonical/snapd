#!/bin/bash

set -eu

fmt=""

for dir in $(go list -f '{{.Dir}}' ./...); do
    s="$(gofmt -s -d "$dir" || true)"
    if [ -n "$s" ]; then
        fmt="$s\\n$fmt"
    fi
done

if [ -n "$fmt" ]; then
    echo "Formatting wrong in following files:"
    # shellcheck disable=SC2001
    echo "$fmt" | sed -e 's/\\n/\n/g'
    exit 1
fi
