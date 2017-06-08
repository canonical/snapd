#!/bin/bash


create_snapd_config_classic() {
    mkdir -p /etc/systemd/system/snapd.service.d
    cat <<EOF > /etc/systemd/system/snapd.service.d/local.conf
[Unit]
StartLimitInterval=0
[Service]
Environment=SNAPD_DEBUG_HTTP=7 SNAPD_DEBUG=1 SNAPPY_TESTING=1 SNAPD_CONFIGURE_HOOK_TIMEOUT=30s
EOF
    mkdir -p /etc/systemd/system/snapd.socket.d
    cat <<EOF > /etc/systemd/system/snapd.socket.d/local.conf
[Unit]
StartLimitInterval=0
EOF
}