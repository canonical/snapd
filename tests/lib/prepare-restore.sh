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
    # We want to rotate the logs so that when inspecting or dumping them we
    # will just see logs since the test has started.

    # Clear the systemd journal. Unfortunately the deputy-systemd on Ubuntu
    # 14.04 does not know about --rotate or --vacuum-time so we need to remove
    # the journal the hard way.
    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*)
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
            ;;
        *)
            journalctl --rotate
            sleep .1
            journalctl --vacuum-time=1ms
            ;;
    esac

    # Clear the kernel ring buffer.
    dmesg -c > /dev/null
}

restore_project_each() {
    # Udev rules are notoriously hard to write and seemingly correct but subtly
    # wrong rules can pass review. Whenever that happens udev logs an error
    # message. As a last resort from lack of a better mechanism we can try to
    # pick up such errors.
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
            snap set core refresh.schedule=""
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
