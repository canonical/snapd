#!/bin/bash

wait_for_file() {
    the_file="$1"
    iters="$2"
    sleep_time="$3"

    for _ in $(seq "$iters"); do
        if [ -f "$the_file" ]; then
            return 0
        fi
        sleep "$sleep_time"
    done
    return 1
}
