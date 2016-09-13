#!/bin/sh
prepare(){
    if [ "$STORE_TYPE" = "fake" ]; then
        echo "Given a snap is installed"
        snap install test-snapd-tools
    fi

    . $TESTSLIB/store.sh
    setup_store $STORE_TYPE $BLOB_DIR

    if [ "$STORE_TYPE" = "fake" ]; then
        echo "And a new version of that snap put in the controlled store"
        fakestore -dir $BLOB_DIR -make-refreshable test-snapd-tools
    fi
}

restore(){
    . $TESTSLIB/store.sh
    teardown_store $STORE_TYPE $BLOB_DIR
}

execute(){
    # FIXME: currently the --list from channel doesn't work
    # echo "Then the new version is available for the snap to be refreshed"
    # expected="$SNAP_NAME +$SNAP_VERSION_PATTERN"
    # snap refresh --list | grep -Pzq "$expected"
    #
    # echo "================================="

    echo "When the snap is refreshed"
    snap refresh --channel=edge $SNAP_NAME

    echo "Then the new version is listed"
    expected="$SNAP_NAME +$SNAP_VERSION_PATTERN"
    snap list | grep -Pzq "$expected"
}
