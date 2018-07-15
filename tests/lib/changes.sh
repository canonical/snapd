#!/bin/bash

change_id() {
    # takes <summary pattern> [<status>]
    local SUMMARY_PAT=$1
    local STATUS=${2:-}
    snap changes|grep -o -P "^\\d+(?= *${STATUS}.*${SUMMARY_PAT}.*)"
}
