#!/bin/bash

# Helpers for replacing the proper random number generator with faster, but less
# secure one and restoring it back. See
# http://elixir.free-electrons.com/linux/latest/source/Documentation/admin-guide/devices.txt
# for major:minor assignments.

fixup_dev_random() {
    rm -f /dev/random /dev/real_random
    # same as /dev/random
    mknod /dev/real_random c 1 8
    # same as /dev/urandom
    mknod /dev/random c 1 9
}

restore_dev_random() {
    rm -f /dev/random /dev/real_random
    # restore proper /dev/random
    mknod /dev/random c 1 8
}
