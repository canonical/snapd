#!/bin/bash

prepare_project() {
    # Check if running inside a container.
    # The testsuite will not work in such an environment
    if systemd-detect-virt -c; then
        echo "Tests cannot run inside a container"
        exit 1
    fi

    "$TESTSLIB"/prepare-project.sh
}

prepare_project_each() {
    # systemd on 14.04 does not know about --rotate or --vacuum-time.
    if [[ "$SPREAD_SYSTEM" != ubuntu-14.04-* ]]; then
        journalctl --rotate
        sleep .1
        journalctl --vacuum-time=1ms
    else
        # Force a log rotation with small size
        sed -i.bak s/#SystemMaxUse=/SystemMaxUse=1K/g /etc/systemd/journald.conf
        systemctl kill --kill-who=main --signal=SIGUSR2 systemd-journald.service

        # Restore the initial configuration and rotate logs
        mv /etc/systemd/journald.conf.bak /etc/systemd/journald.conf
        systemctl kill --kill-who=main --signal=SIGUSR2 systemd-journald.service

        # Remove rotated journal logs
        systemctl stop systemd-journald.service
        find /run/log/journal/ -name "*@*.journal" -delete
        systemctl start systemd-journald.service
    fi
    dmesg -c > /dev/null
}

restore_project_each() {
    if grep "invalid .*snap.*.rules" /var/log/syslog; then
        echo "Invalid udev file detected, test most likely broke it"
        exit 1
    fi
}

restore_project() {
    if [ "$SPREAD_BACKEND" = external ]; then
        # start and enable autorefresh
        if [ -e /snap/core/current/meta/hooks/configure ]; then
            systemctl enable --now snapd.refresh.timer
            snap set core refresh.disabled=false
        fi
    fi

    rm -f "$SPREAD_PATH/snapd-state.tar.gz"
    if [ -n "$GOPATH" ]; then
        rm -rf "${GOPATH%%:*}"
    fi
}

case "$1" in
    --prepare-project)
        prepare_project
        ;;
    --prepare-project-each)
        prepare_project_each
        ;;
    --restore-project-each)
        restore_project_each
        ;;
    --restore-project)
        restore_project
        ;;
    *)
        echo "unsupported argument: $1"
        echo "try one of --{prepare,restore}-project{,-each}"
        exit 1
        ;;
esac
