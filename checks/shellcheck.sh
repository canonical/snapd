#!/bin/bash

set -eu

exclude_tools_path=tests/lib/external/snapd-testing-tools

INITIAL_FILES="$( find . \( -name .git -o -name vendor -o -name c-vendor \) -prune -o -print0 | xargs -0 file -N | awk -F": " '$2~/shell.script/{print $1}')"

FILTERED_FILES=
for file in $INITIAL_FILES; do
    if ! echo "$file" | grep -q "$exclude_tools_path"; then
        FILTERED_FILES="$FILTERED_FILES $file"
    fi
done

# shellcheck disable=SC2086
shellcheck -x $FILTERED_FILES
