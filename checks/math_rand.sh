#!/bin/bash

set -eu

got=""
for dir in $(go list -f '{{.Dir}}' ./...); do
    # shellcheck disable=SC2063
    s="$(grep -nP --exclude '*_test.go' --exclude tests/lib/plz-run/main.go --exclude 'randutil/*.go' math/rand "$dir"/*.go || true)"
    if [ -n "$s" ]; then
        got="$s\\n$got"
    fi
done

if [ -n "$got" ]; then
    echo 'Direct usages of math/rand, we prefer randutil:' >&2
    echo "$got" >&2
    exit 1
fi
