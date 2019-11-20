#!/bin/sh -e

check_state_json() {
    JQ_FILTER=$1
    COMP=$2
    RES=$3
    FAIL_MSG=$4
    # we are storing jq filter specs in JQFILTER, so we can't use --arg like 
    # normal with jq, so instead just do a _real_ simple and potentially 
    # _really_ annoying string concatenation with the actual verbatim variable
    if [ "$COMP" = "!=" ]; then
        if [ "$(jq -r '.data.snaps."disabled-svcs-kept" | '"$JQ_FILTER" < /var/lib/snapd/state.json)" != "$RES" ]; then
            echo "$FAIL_MSG"
            jq -r '.data.snaps."disabled-svcs-kept"' < /var/lib/snapd/state.json
            exit 1
        fi
    elif [ "$COMP" = "=" ]; then
        if [ "$(jq -r '.data.snaps."disabled-svcs-kept" | '"$JQ_FILTER" < /var/lib/snapd/state.json)" = "$RES" ]; then
            echo "$FAIL_MSG"
            jq -r '.data.snaps."disabled-svcs-kept"' < /var/lib/snapd/state.json
            exit 1
        fi
    else
        echo "invalid comparison operator"
        exit 1
    fi
}

check_state_json_no_disabled_svcs() {
    check_state_json \
        '."last-active-disabled-services"?' \
        "!=" \
        "null" \
        "state.json has invalid last-active-disabled-services in it:"
}

check_state_json_yes_disabled_svcs() {
    check_state_json \
        '."last-active-disabled-services"?' \
        "=" \
        "null" \
        "state.json has invalid last-active-disabled-services in it:"
}

SVC_MISSING_ERR_MSG="state.json is missing last-active-disabled-services in it:"

check_state_json_specific_disabled_svc() {
    SVC=$1
    check_state_json \
        '."last-active-disabled-services"? | .[]' \
        "!=" \
        "$SVC" \
        "$SVC_MISSING_ERR_MSG"
}
