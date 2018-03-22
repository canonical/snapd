#!/bin/sh
# Some tests may require randomness, for example to create an encryption
# key. Because true randomness is hard to come by (especially in test
# virtual machines) we are replacing /dev/random with the equivalent of
# /dev/urandom so that tests don't block.

# shellcheck source=tests/lib/random.sh
. "$TESTSLIB/random.sh"

on_prepare_project_each() {
    fixup_dev_random
}

on_restore_project_each() {
    restore_dev_random
}
