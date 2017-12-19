#!/bin/bash

set +x

execution_time_ms(){
    command="$1"
    ts=$(date +%s%N)
    ( $command ) > /dev/null 2>&1
    et=$((($(date +%s%N) - $ts)/1000000))
    echo $et
}

create_results_file(){
    file="$1"
    echo "#####START-PERF#####" > $file
}

finish_results_file(){
    file="$1"
    echo "#####END-PERF#####" >> $file
}
