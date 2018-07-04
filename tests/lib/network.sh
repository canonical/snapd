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
    SERVICE_FILE="$2"
    PORT="$3"

    #shellcheck source=tests/lib/systemd.sh
    . "$TESTLIB"/systemd.sh

    printf '#!/bin/sh -e\nwhile true; do printf '\''HTTP/1.1 200 OK\\n\\nok\\n'\'' |  nc -l -p %s -w 1; done' "$PORT" > "$SERVICE_FILE"
    chmod a+x "$SERVICE_FILE"
    systemd_create_and_start_unit "$SERVICE_NAME" "$(readlink -f "$SERVICE_FILE")"
    wait_listen_port "$PORT"
}
