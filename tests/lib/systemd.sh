#!/bin/bash

# Use like systemd_create_and_start_unit(fakestore, "$(which fakestore) -start -dir $top_dir -addr localhost:11028 $@")
systemd_create_and_start_unit() {
    printf "[Unit]\nDescription=For testing purposes\n[Service]\nType=simple\nExecStart=%s\n" "$2" > /run/systemd/system/$1.service
    if [ -n "${3:-}" ]; then
        echo "Environment=$3" >> /run/systemd/system/$1.service
    fi
    systemctl daemon-reload
    systemctl start $1
}

# Use like systemd_stop_and_destroy_unit(fakestore)
systemd_stop_and_destroy_unit() {
    if systemctl status "$1"; then
        systemctl stop "$1"
    fi
    rm -f /run/systemd/system/$1.service
    systemctl daemon-reload
}
