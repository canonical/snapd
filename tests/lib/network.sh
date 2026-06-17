#!/bin/bash

wait_listen_port(){
    PORT="$1"
    NETNS="${2:-}"

    for _ in $(seq 120); do
        if ${NETNS:+ip netns exec "$NETNS"} ss -lnt | grep -Pq "LISTEN.*?:$PORT +.*?\\n*"; then
            break
        fi
        sleep 0.5
    done

    # Ensure we really have the listen port, this will fail with an
    # exit code if the port is not available.
    ${NETNS:+ip netns exec "$NETNS"} ss -lnt | grep -Pq "LISTEN.*?:$PORT +.*?\\n*"
}

make_network_service() {
    SERVICE_NAME="$1"
    PORT="$2"
    NETNS="${3:-}"

    local netns_prop=""
    if [ -n "$NETNS" ]; then
        netns_prop="--property NetworkNamespacePath=/run/netns/${NETNS}"
    fi
    # shellcheck disable=SC2086
    systemd-run --unit "$SERVICE_NAME" $netns_prop sh -c "while true; do printf 'HTTP/1.1 200 OK\\n\\nok\\n' |  nc -l -p $PORT -w 1; done"
    wait_listen_port "$PORT" "$NETNS"
}
