#!/bin/bash

set +x

measure_exec_time() {
    command=$1
    test=$2
    scenario=$3

    et=$(execution_time_ms "$command")
    echo "$SPREAD_SYSTEM,$test,$scenario,$et,milliseconds" >> "$PERF_RESULTS_FILE"
}

execution_time_ms() {
    command=$1
    ts=$(date +%s%N)
    ( "$command" ) > /dev/null 2>&1
    et=$((($(date +%s%N) - $ts)/1000000))
    echo "$et"
}

create_results_file() {
    echo "#####START-PERF#####" > "$PERF_RESULTS_FILE"
}

finish_results_file() {
    echo "#####END-PERF#####" >> "$PERF_RESULTS_FILE"
    cat "$PERF_RESULTS_FILE"
}

clean_results_file() {
    rm -f "$PERF_RESULTS_FILE"
}
