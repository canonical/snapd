#!/bin/bash

set -eu

got=""
for dir in $(go list -f '{{.Dir}}' ./...); do
    # shellcheck disable=SC2063
    s="$(grep -nP io/ioutil "$dir"/*.go || true)"
    if [ -n "$s" ]; then
        got="$s\\n$got"
    fi
done

if [ -n "$got" ]; then
    echo 'Found usages of deprecated io/ioutil, please use "io" or "os" equivalents' >&2
    echo "$got" >&2
    exit 1
fi
