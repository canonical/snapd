#!/bin/sh -e

check_state_json_no_disabled_svcs() {
    "$TESTSTOOLS"/snapd-state check-state \
        '.data.snaps."disabled-svcs-kept" | ."last-active-disabled-services"?' \
        "=" \
        "null" \
        "state.json has invalid last-active-disabled-services in it:"
}

check_state_json_yes_disabled_svcs() {
    "$TESTSTOOLS"/snapd-state check-state \
        '.data.snaps."disabled-svcs-kept" | ."last-active-disabled-services"?' \
        "!=" \
        "null" \
        "state.json has invalid last-active-disabled-services in it:"
}

SVC_MISSING_ERR_MSG="state.json is missing last-active-disabled-services in it:"

check_state_json_specific_disabled_svc() {
    SVC=$1
    "$TESTSTOOLS"/snapd-state check-state \
        '.data.snaps."disabled-svcs-kept" | ."last-active-disabled-services"? | .[]' \
        "=" \
        "$SVC" \
        "$SVC_MISSING_ERR_MSG"
}
