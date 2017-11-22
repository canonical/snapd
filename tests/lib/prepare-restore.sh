#!/bin/bash

prepare_project() {
    # Check if running inside a container.
    # The testsuite will not work in such an environment
    if systemd-detect-virt -c; then
        echo "Tests cannot run inside a container"
        exit 1
    fi

    # FIXME: remove once the following bug is fixed:
    #   https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=876128
    if [[ "$SPREAD_SYSTEM" == debian-unstable-* ]]; then
        # There's a packaging bug in Debian sid lately where some packages
        # conflict on manual page file. To work around it simply remove the
        # manpages package.
        apt-get remove --purge -y manpages
    fi

    # apt update is hanging on security.ubuntu.com with IPv6, prefer IPv4 over IPv6
    cat <<EOF > gai.conf
precedence  ::1/128       50
precedence  ::/0          40
precedence  2002::/16     30
precedence ::/96          20
precedence ::ffff:0:0/96 100
EOF
    if ! mv gai.conf /etc/gai.conf; then
        echo "/etc/gai.conf is not writable, ubuntu-core system? apt update won't be affected in that case"
        rm -f gai.conf
    fi

    # Unpack delta, or move content out of the prefixed directory (see rename and repack above).
    # (needs to be in spread.yaml directly because there's nothing else on the filesystem yet)
    if [ -f current.delta ]; then
        tf=$(mktemp)
        # NOTE: We can't use tests/lib/pkgdb.sh here as it doesn't exist at
        # this time when none of the test files is yet in place.
        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                apt-get update >& "$tf" || ( cat "$tf"; exit 1 )
                apt-get install -y xdelta3 curl >& "$tf" || ( cat "$tf"; exit 1 )
                ;;
            fedora-*)
                dnf install --refresh -y xdelta curl &> "$tf" || (cat "$tf"; exit 1)
                ;;
            opensuse-*)
                zypper -q install -y xdelta3 curl &> "$tf" || (cat "$tf"; exit 1)
                ;;
        esac
        rm -f "$tf"
        curl -sS -o - "https://codeload.github.com/snapcore/snapd/tar.gz/$DELTA_REF" | gunzip > delta-ref.tar
        xdelta3 -q -d -s delta-ref.tar current.delta | tar x --strip-components=1
        rm -f delta-ref.tar current.delta
    elif [ -d "$DELTA_PREFIX" ]; then
        find "$DELTA_PREFIX" -mindepth 1 -maxdepth 1 -exec mv {} . \;
        rmdir "$DELTA_PREFIX"
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
