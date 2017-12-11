#!/bin/bash

# Helpers for replacing the proper random number generator with faster, but less
# secure one and restoring it back. See
# http://elixir.free-electrons.com/linux/latest/source/Documentation/admin-guide/devices.txt
# for major:minor assignments.

kill_gpg_agent() {
    # gpg-agent might have started before, need to kill it, normally we would
    # call gpgconf --kill gpg-agent but this does not seem 100% reliable, try
    # more direct approach; if gpg-agent gets blocked reading from /dev/random
    # it will not react to SIGTERM, use SIGKILL instead
    pkill -9 -e gpg-agent || true
}

fixup_dev_random() {
    # keep  the original /dev/random around
    mv /dev/random /dev/random.orig
    # same as /dev/urandom
    mknod /dev/random c 1 9
    # make sure that gpg-agent picks up the new device
    kill_gpg_agent
}

restore_dev_random() {
    if test -c /dev/random.orig ; then
        mv /dev/random.orig /dev/random
    fi
}

debug_random() {
    sysctl kernel.random.entropy_avail || true
    ls -l /dev/*random*
    pids=$(pidof gpg-agent)
    for p in $pids; do
        ps -q "$p"
        ls -l "/proc/$p/fd"
    done
}
