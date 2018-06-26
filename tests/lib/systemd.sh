#!/bin/bash

# Use like systemd_create_and_start_unit(fakestore, "$(which fakestore) -start -dir $top_dir -addr localhost:11028 $@")
systemd_create_and_start_unit() {
    printf "[Unit]\nDescription=For testing purposes\n[Service]\nType=simple\nExecStart=%s\n" "$2" > "/run/systemd/system/$1.service"
    if [ -n "${3:-}" ]; then
        echo "Environment=$3" >> "/run/systemd/system/$1.service"
    fi
    systemctl daemon-reload
    systemctl start "$1"
}

# Use like systemd_stop_and_destroy_unit(fakestore)
systemd_stop_and_destroy_unit() {
    if systemctl is-active "$1"; then
        systemctl stop "$1"
    fi
    rm -f "/run/systemd/system/$1.service"
    systemctl daemon-reload
}

wait_for_service() {
    local service_name="$1"
    local state="${2:-active}"
    for i in $(seq 300); do
        if systemctl show -p ActiveState "$service_name" | grep -q "ActiveState=$state"; then
            return
        fi
        # show debug output every 1min
        if [ $(( i % 60 )) = 0 ]; then
            systemctl status "$service_name" || true;
        fi
        sleep 1;
    done

    echo "service $service_name did not start"
    exit 1
}

systemd_stop_units() {
    for unit in "$@"; do
        if systemctl is-active "$unit"; then
            echo "Ensure the service is active before stopping it"
            retries=20
            systemctl status "$unit" || true
            while systemctl status "$unit" | grep "Active: activating"; do
                if [ $retries -eq 0 ]; then
                    echo "$unit unit not active"
                    exit 1
                fi
                retries=$(( retries - 1 ))
                sleep 1
            done

            systemctl stop "$unit"
        fi
    done
}

systemd_get_active_snapd_units() {
    systemctl list-units --plain --state=active|grep -Eo '^snapd\..*(socket|service|timer)' || true
}
