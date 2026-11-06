#!/bin/bash

set -eu

forbidden_deps=(
    "github.com/snapcore/snapd/testutil"
    "gopkg.in/check.v1"
)
go_list_cmd=(go list)

if [ -n "${GO_BUILD_TAGS:-}" ]; then
    go_list_cmd+=(-tags "$GO_BUILD_TAGS")
fi

cmd_mains=$("${go_list_cmd[@]}" -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./cmd/...)

violations=()

for cmd_main in $cmd_mains; do
    deps=$("${go_list_cmd[@]}" -deps "$cmd_main")

    for dep in "${forbidden_deps[@]}"; do
        if grep -qx "$dep" <<<"$deps"; then
            violations+=("$cmd_main -> $dep")
        fi
    done
done

if [ ${#violations[@]} -gt 0 ]; then
    echo "Go binaries must not depend on forbidden packages:" >&2
    for dep in "${forbidden_deps[@]}"; do
        echo "  - $dep" >&2
    done
    echo "Violations:" >&2
    for violation in "${violations[@]}"; do
        echo "  - $violation" >&2
    done
    exit 1
fi
