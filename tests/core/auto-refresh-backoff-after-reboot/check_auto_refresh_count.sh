#!/bin/bash

LAST_CHANGE_ID=$1
CHANGES_COUNT=$2

#shellcheck disable=SC2086,SC2046
test $(snap debug api /v2/changes?select=ready | gojq "[.result[] | select(.kind == \"auto-refresh\" and (.id|tonumber) > ($LAST_CHANGE_ID|tonumber))] | length") == $CHANGES_COUNT
