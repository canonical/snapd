#!/bin/bash

get_default_iface(){
    ip route get 8.8.8.8 | awk '{ print $5; exit }'
}

wait_listen_port(){
    PORT="$1"

    for _ in $(seq 120); do
        if ss -lnt | grep -Pq "LISTEN.*?:$PORT +.*?\\n*"; then
            break
        fi
        sleep 0.5
    done

    # Ensure we really have the listen port, this will fail with an
    # exit code if the port is not available.
    ss -lnt | grep -Pq "LISTEN.*?:$PORT +.*?\\n*"
}

make_network_service() {
    SERVICE_NAME="$1"
    PORT="$2"

    systemd-run --unit "$SERVICE_NAME" sh -c "while true; do printf 'HTTP/1.1 200 OK\\n\\nok\\n' |  nc -l -p $PORT -w 1; done"
    wait_listen_port "$PORT"
}
