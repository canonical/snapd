#!/bin/sh
# enter classic ubuntu environment on snappy
# Author: Martin Pitt <martin.pitt@ubuntu.com>

set -eu

ROOT=/writable/classic

if [ "$(id -u)" != "0" ]; then
    echo "needs to run as root"
    exit 1
fi

# $1: source
# $2: target
# $3: if not empty, bind mount will be read-only
# note that we do NOT clean these up at the end, as the user might start many
# classic shells in parallel; we could start all of them in their own mount
# namespace, but that would make the classic shell less useful for
# developing/debugging the snappy host
do_bindmount() {
    if ! mountpoint -q "$ROOT/$2"; then
        if [ -d "$1" -a ! -L "$1" ]; then
            mkdir -p "$ROOT/$2"
        fi
        mount --make-rprivate --rbind -o rbind "$1" "$ROOT/$2"
        if [ -n "${3:-}" ]; then
            mount --rbind -o remount,ro "$1" "$ROOT/$2"
        fi
    fi
}

do_bindmount /home /home
do_bindmount /run /run
do_bindmount /proc /proc
do_bindmount /sys /sys
do_bindmount /dev /dev
do_bindmount /var/lib/extrausers /var/lib/extrausers "ro"
do_bindmount /etc/sudoers /etc/sudoers "ro"
do_bindmount /etc/sudoers.d /etc/sudoers.d "ro"
do_bindmount / /snappy

if [ -z "$SUDO_USER" ]; then
    echo "Cannot determine calling user, logging into classic as root"
    SUDO_USER=root
fi

systemd-run --quiet --scope --unit=snappy-classic.scope --description="Snappy Classic shell" chroot "$ROOT" sudo debian_chroot="classic" -u $SUDO_USER -i
# kill leftover processes after exiting, if it's still around
systemctl stop snappy-classic.scope 2>/dev/null || true
