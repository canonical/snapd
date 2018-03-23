#!/bin/bash
# XXX: This still needs a rewrite to better integrate into prepare-restore.d
# but for now it's a nice incremental upgrade. The idea is that we setup the
# heavy lifting once and create a tarball that contains all of those things we
# made. Instead of cleaning up we remove files and unpack the tarball.  The
# concept is nice but a little bit flawed because the prepare/restore phases
# are not aligned with spread concepts and there's no single place where we can
# measure pristine state and ensure the system is restored to that state.
#
# This idea should be evolved so that prepare unpacks the tarball, restore
# removes all the side effects and there's a consistent state otherwise (before
# prepare is identical to after restore). This can be enforced automatically
# once https://github.com/snapcore/spread/pull/47 or equivalent is merged and
# when spread offers a way to measure the environment and ensure it stays the
# same between tests.

on_prepare_suite() {
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-16-* ]]; then
        prepare_all_snap
    else
        prepare_classic
    fi
}

on_prepare_suite_each() {
    # shellcheck source=tests/lib/reset.sh
    "$TESTSLIB"/reset.sh --reuse-core
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    if [[ "$SPREAD_SYSTEM" != ubuntu-core-16-* ]]; then
        prepare_each_classic
    fi
}

on_restore_project() {
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
}
