#!/bin/bash
set -x
# NOTE: We must set -e so that any failures coming out of the various
# statements we execute stops the build. The code is not (yet) written to
# handle errors in general.
set -e
# Set pipefail option so that "foo | bar" behaves with fewer surprises by
# failing if foo fails, not just if bar fails.
set -o pipefail

# shellcheck source=tests/lib/quiet.sh
. "$TESTSLIB/quiet.sh"

# XXX: boot.sh has side-effects
# shellcheck source=tests/lib/boot.sh
. "$TESTSLIB/boot.sh"

# XXX: dirs.sh has side-effects
# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB/pkgdb.sh"

# shellcheck source=tests/lib/random.sh
. "$TESTSLIB/random.sh"

# shellcheck source=tests/lib/spread-funcs.sh
. "$TESTSLIB/spread-funcs.sh"

###
### Utility functions reused below.
###

# Run a set of scripts in the prepare-restore.d directory. From each
# script run, if present, the function on_$phase, where phase is one of
# {prepare,restore}_{project,suite}{,_each}.
do_phase() {
    phase="$1" && shift
    for module in "$TESTSLIB"/prepare-restore.d/*.sh; do
        (
            set -u
            set -e
            # shellcheck disable=SC1090
            . "$module"
            if [ "$(type -t "on_$phase")" = "function" ]; then
                "on_$phase"
            fi
        )
    done
}

###
### Prepare / restore functions for {project,suite}
###

prepare_project() {
    do_phase prepare_project

    # Set REUSE_PROJECT to reuse the previous prepare when also reusing the server.
    [ "$REUSE_PROJECT" != 1 ] || exit 0
    echo "Running with SNAP_REEXEC: $SNAP_REEXEC"

    # check that we are not updating
    if [ "$(bootenv snap_mode)" = "try" ]; then
        echo "Ongoing reboot upgrade process, please try again when finished"
        exit 1
    fi

    # declare the "quiet" wrapper

    if [ "$SPREAD_BACKEND" = external ]; then
        chown test.test -R "$PROJECT_PATH"
        exit 0
    fi



    # Build fakestore.
    fakestore_tags=
    if [ "$REMOTE_STORE" = staging ]; then
        fakestore_tags="-tags withstagingkeys"
    fi

    # eval to prevent expansion errors on opensuse (the variable keeps quotes)
    eval "go get $fakestore_tags ./tests/lib/fakestore/cmd/fakestore"

    # Build additional utilities we need for testing
    go get ./tests/lib/fakedevicesvc
    go get ./tests/lib/systemd-escape

    # disable journald rate limiting
    mkdir -p /etc/systemd/journald.conf.d/
    cat <<-EOF > /etc/systemd/journald.conf.d/no-rate-limit.conf
    [Journal]
    RateLimitIntervalSec=0
    RateLimitBurst=0
EOF
    systemctl restart systemd-journald.service
}

prepare_project_each() {
    do_phase prepare_project_each

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
            # per journalctl's implementation, --rotate and --sync 'override'
            # each other if used in a single command, with the one appearing
            # later being effective
            journalctl --sync
            journalctl --rotate
            sleep .1
            journalctl --vacuum-time=1ms
            ;;
    esac

    # Clear the kernel ring buffer.
    dmesg -c > /dev/null

    fixup_dev_random
}

prepare_suite() {
    do_phase prepare_suite

    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-16-* ]]; then
        prepare_all_snap
    else
        prepare_classic
    fi
}

prepare_suite_each() {
    do_phase prepare_suite_each

    # shellcheck source=tests/lib/reset.sh
    "$TESTSLIB"/reset.sh --reuse-core
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    if [[ "$SPREAD_SYSTEM" != ubuntu-core-16-* ]]; then
        prepare_each_classic
    fi
}

restore_suite_each() {
    do_phase restore_suite_each
}

restore_suite() {
    do_phase restore_suite

    # shellcheck source=tests/lib/reset.sh
    "$TESTSLIB"/reset.sh --store
    if [[ "$SPREAD_SYSTEM" != ubuntu-core-16-* ]]; then
        # shellcheck source=tests/lib/pkgdb.sh
        . $TESTSLIB/pkgdb.sh
        distro_purge_package snapd
        if [[ "$SPREAD_SYSTEM" != opensuse-* ]]; then
            # A snap-confine package never existed on openSUSE
            distro_purge_package snap-confine
        fi
    fi
}

restore_project_each() {
    do_phase restore_project_each

    restore_dev_random

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
    do_phase restore_project

    # We use a trick to accelerate prepare/restore code in certain suites. That
    # code uses a tarball to store the vanilla state. Here we just remove this
    # tarball.
    rm -f "$SPREAD_PATH/snapd-state.tar.gz"

    # Remove all of the code we pushed and any build results. This removes
    # stale files and we cannot do incremental builds anyway so there's little
    # point in keeping them.
    if [ -n "$GOPATH" ]; then
        rm -rf "${GOPATH%%:*}"
    fi

    rm -rf /etc/systemd/journald.conf.d/no-rate-limit.conf
    rmdir /etc/systemd/journald.conf.d || true
}

case "$1" in
    --prepare-project)
        prepare_project
        ;;
    --prepare-project-each)
        prepare_project_each
        ;;
    --prepare-suite)
        prepare_suite
        ;;
    --prepare-suite-each)
        prepare_suite_each
        ;;
    --restore-suite-each)
        restore_suite_each
        ;;
    --restore-suite)
        restore_suite
        ;;
    --restore-project-each)
        restore_project_each
        ;;
    --restore-project)
        restore_project
        ;;
    *)
        echo "unsupported argument: $1"
        echo "try one of --{prepare,restore}-{project,suite}{,-each}"
        exit 1
        ;;
esac
