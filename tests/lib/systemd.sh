#!/bin/bash

# Use like systemd_create_and_start_unit(fakestore, "$(which fakestore) -start -dir $top_dir -addr localhost:11028 $@")
systemd_create_and_start_unit() {
    printf '[Unit]\nDescription=Support for test %s\n[Service]\nType=simple\nExecStart=%s\n' "${SPREAD_JOB:-unknown}" "$2" > "/run/systemd/system/$1.service"
    if [ -n "${3:-}" ]; then
        echo "Environment=$3" >> "/run/systemd/system/$1.service"
    fi
    systemctl daemon-reload
    systemctl start "$1"
}

# Create and start a persistent systemd unit that survives reboots. Use as:
#   systemd_create_and_start_persistent_unit "name" "my-service --args"
# The third arg supports "overrides" which allow to customize the service
# as needed, e.g.:
#   systemd_create_and_start_persistent_unit "name" "start" "[Unit]\nAfter=foo"
systemd_create_and_start_persistent_unit() {
    printf '[Unit]\nDescription=Support for test %s\n[Service]\nType=simple\nExecStart=%s\n[Install]\nWantedBy=multi-user.target\n' "${SPREAD_JOB:-unknown}" "$2" > "/etc/systemd/system/$1.service"
    if [ -n "${3:-}" ]; then
        mkdir -p "/etc/systemd/system/$1.service.d"
        # shellcheck disable=SC2059
        printf "$3" >> "/etc/systemd/system/$1.service.d/override.conf"
    fi
    systemctl daemon-reload
    systemctl enable "$1"
    systemctl start "$1"
    wait_for_service "$1"
}

system_stop_and_remove_persistent_unit() {
    systemctl stop "$1" || true
    systemctl disable "$1" || true
    rm -f "/etc/systemd/system/$1.service"
    rm -rf "/etc/systemd/system/$1.service.d"
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
        if [ "$i" -gt 0 ] && [ $(( i % 60 )) = 0 ]; then
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
            while systemctl status "$unit" | grep -q "Active: activating"; do
                if [ $retries -eq 0 ]; then
                    echo "$unit unit not active"
                    systemctl status "$unit" || true
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
