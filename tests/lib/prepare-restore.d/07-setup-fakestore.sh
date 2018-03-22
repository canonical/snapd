#!/bin/sh
# Some tests need to talk to a fake store to emulate refreshes and other
# store-side operations. We build the fake store from source when preparing the
# project.

on_prepare_project() {
    fakestore_tags=
    if [ "$REMOTE_STORE" = staging ]; then
        fakestore_tags="-tags withstagingkeys"
    fi

    # eval to prevent expansion errors on opensuse (the variable keeps quotes)
    eval "go get $fakestore_tags ./tests/lib/fakestore/cmd/fakestore"
}

on_restore_suite() {
    # shellcheck source=tests/lib/reset.sh
    "$TESTSLIB"/reset.sh --store
}
