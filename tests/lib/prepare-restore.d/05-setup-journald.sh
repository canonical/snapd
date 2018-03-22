#!/bin/bash

on_prepare_project_each() {
    # We want to rotate the logs so that when inspecting or dumping them we
    # will just see logs since the test has started.

    # Clear the systemd journal. Unfortunately the deputy-systemd on Ubuntu
    # 14.04 does not know about --rotate or --vacuum-time so we need to remove
    # the journal the hard way.
    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*)
            # Force a log rotation by setting a small maximum size.
            sed -i.bak 's/#SystemMaxUse=/SystemMaxUse=1K/g' /etc/systemd/journald.conf
            systemctl kill --kill-who=main --signal=SIGUSR2 systemd-journald.service

            # Restore the initial configuration and rotate logs.
            mv /etc/systemd/journald.conf.bak /etc/systemd/journald.conf
            systemctl kill --kill-who=main --signal=SIGUSR2 systemd-journald.service

            # Remove rotated journal logs.
            systemctl stop systemd-journald.service
            find /run/log/journal/ -name "*@*.journal" -delete
            systemctl start systemd-journald.service
            ;;
        *)
            # Per journalctl's implementation, --rotate and --sync 'override'
            # each other if used in a single command, with the one appearing
            # later being effective.
            journalctl --sync
            journalctl --rotate
            sleep .1
            journalctl --vacuum-time=1ms
            ;;
    esac

    # Disable journal rate limiting. This lets us capture all the messages even
    # if the burst rate is very high.
    mkdir -p /etc/systemd/journald.conf.d/
    cat <<-EOF > /etc/systemd/journald.conf.d/no-rate-limit.conf
    [Journal]
    RateLimitIntervalSec=0
    RateLimitBurst=0
EOF
    systemctl restart systemd-journald.service
}

on_restore_project() {
    rm -rf /etc/systemd/journald.conf.d/no-rate-limit.conf
    rmdir /etc/systemd/journald.conf.d || true
}
