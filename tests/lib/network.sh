#!/bin/bash

get_default_iface(){
    ip route get 8.8.8.8 | awk '{ print $5; exit }'
}

wait_listen_port(){
    PORT="$1"

    for _ in $(seq 120); do
        if ss -lnt | grep -Pq "LISTEN.*?:$PORT +.*?\n*"; then
            break
        fi
        sleep 0.5
    done

    # Ensure we really have the listen port, this will fail with an
    # exit code if the port is not available.
    ss -lnt | grep -Pq "LISTEN.*?:$PORT +.*?\n*"
}
