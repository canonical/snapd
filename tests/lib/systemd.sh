#!/bin/bash

# Use like systemd_create_and_start_unit(fakestore, "$(which fakestore) -start -dir $top_dir -addr localhost:11028 $@")
systemd_create_and_start_unit() {
    printf "[Unit]\nDescription=For testing purposes\n[Service]\nType=simple\nExecStart=%s" "$2" > /run/systemd/system/$1.service
    systemctl daemon-reload
    systemctl start $1
}

# Use like systemd_stop_and_destroy_unit(fakestore)
systemd_stop_and_destroy_unit() {
    systemctl stop $1
    rm /run/systemd/system/$1.service
    systemctl daemon-reload
}
