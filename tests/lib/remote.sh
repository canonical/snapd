#!/bin/bash

download_file(){
    local url=$1
    local output_name=${2:-""}
    local other_options=${3:-""}
    local options

    if [ -n "$output_name" ]; then
        options="-o $output_name"
    fi

    if [ -n "$other_options" ]; then
        if echo "$other_options" | grep -q "follow-redirects"; then
            options="$options -L"
        fi
    fi

    for _ in $(seq 5); do
        if curl -s "$options" "$url"; then
            return 0
        fi
        echo "Download failed, retrying..."
    done
    return 1
}
