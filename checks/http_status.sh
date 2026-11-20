#!/bin/bash

set -eu

got=""
for dir in $(go list -f '{{.Dir}}' ./...); do
    s="$(grep -nP 'http\.Status(?!Text)' "$dir"/*.go || true)"
    if [ -n "$s" ]; then
        got="$s\\n$got"
    fi
done

if [ -n "$got" ]; then
    echo 'Usages of http.Status*, we prefer the numeric values directly:' >&2
    echo "$got" >&2
    exit 1
fi
