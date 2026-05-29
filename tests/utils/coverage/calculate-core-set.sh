#!/bin/bash

set -euo pipefail

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

echo "{\"user\": \"$MONGO_USER\", \"host\": \"$MONGO_HOST\", \"password\": \"$MONGO_PASSWORD\", \"port\": $MONGO_PORT}" > "$tmpdir"/creds.json

timestamps=$(gh run list --repo canonical/snapd --workflow weekly-feature-tagging.yaml --json event,updatedAt --jq '.[] | select(.event == "schedule") | .updatedAt' | head -2)
date_latest=$(date -d "$(head -1 <<<$timestamps)" "+%Y-%m-%d")
date_second=$(date -d "$(tail -1 <<<$timestamps)" "+%Y-%m-%d")

all_entries=$(tests/utils/features/query_features.py list -f "$tmpdir"/creds.json)

# Since feature tagging runs early in the morning of a day, the day should be the same both when the workflow ends and when the timestamp for mongo was generated.
latest_timestamp=$(jq -r --arg date "$date_latest" 'first(.[] | select(.timestamp | startswith($date))) | .timestamp' <<<"$all_entries")
second_timestamp=$(jq -r --arg date "$date_second" 'first(.[] | select(.timestamp | startswith($date))) | .timestamp' <<<"$all_entries")

if [[ -z "$latest_timestamp" || "$latest_timestamp" == "null" ]]; then
    echo "no entry found for date $date_latest" >&2
    exit 1
fi

if [[ -z "$second_timestamp" || "$second_timestamp" == "null" ]]; then
    echo "no entry found for date $date_second" >&2
    exit 1
fi

latest=$(tests/utils/features/query_features.py feat cover -f "$tmpdir"/creds.json -t "$latest_timestamp")
second=$(tests/utils/features/query_features.py feat cover -f "$tmpdir"/creds.json -t "$second_timestamp")

overview=$(jq -n --argjson latest "$latest" --argjson second "$second" '
    ($latest | keys | sort) as $latest_keys |
    ($second | keys | sort) as $second_keys |
    [ $latest_keys[] | select($second_keys | index(.)) ] as $common |
    [
        $common[] |
        {
            system: .,
            latest_len: ($latest[.] | length),
            second_len: ($second[.] | length),
            delta: (($latest[.] | length) - ($second[.] | length)),
            identical: ($latest[.] == $second[.])
        }
    ] as $rows |
    {
        systems_only_in_latest: ($latest_keys - $second_keys),
        systems_only_in_second: ($second_keys - $latest_keys),
        latest_system_count: ($latest_keys | length),
        second_system_count: ($second_keys | length),
        common_system_count: ($common | length),
        identical_systems: ([ $rows[] | select(.identical) | .system ] | sort),
        identical_system_count: ([ $rows[] | select(.identical) ] | length),
        per_system_length_diff: ([ $rows[] | select(.delta != 0) ] | sort_by(.delta, .system))
    }
')

echo "overview between latest and second" >&2
printf '%s\n' "$overview" >&2

combined=$(jq -n --argjson latest "$latest" --argjson second "$second" '
    reduce (($latest | keys_unsorted)[]) as $system (
        {};
        . + {
            ($system): (((($latest[$system] // []) + ($second[$system] // [])) | unique) | sort)
        }
    )
')

printf '%s\n' "$combined"

rm -rf "$tmpdir"

