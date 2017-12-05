#!/bin/bash

# Helpers for replacing the proper random number generator with faster, but less
# secure one and restoring it back. See
# http://elixir.free-electrons.com/linux/latest/source/Documentation/admin-guide/devices.txt
# for major:minor assignments.

fixup_dev_random() {
    # keep  the original /dev/random around
    mv /dev/random /dev/random.orig
    # same as /dev/urandom
    mknod /dev/random c 1 9
}

restore_dev_random() {
    if test -c /dev/random.orig ; then
        mv /dev/random.orig /dev/random
    fi
}
