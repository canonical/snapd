#!/bin/bash

set -eu

exclude_tools_path=tests/lib/external/snapd-testing-tools

FILTERED_FILES="spread.yaml"
FILTERED_FILES="$FILTERED_FILES tests"

# XXX: exclude core20-preseed test as its environment block confuses shellcheck, and it's not possible to disable shellcheck there.
# shellcheck disable=SC2086
./tests/lib/external/snapd-testing-tools/utils/spread-shellcheck $FILTERED_FILES --exclude "$exclude_tools_path" --exclude "tests/nested/manual/core20-preseed"
