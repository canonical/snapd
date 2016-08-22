#!/bin/sh
STORE_CONFIG=/etc/systemd/system/snapd.service.d/store.conf

_configure_store_backends(){
    systemctl stop snapd.service snapd.socket
    mkdir -p $(dirname $STORE_CONFIG)
    cat > $STORE_CONFIG <<EOF
[Service]
Environment=$*
EOF
    systemctl daemon-reload
    systemctl start snapd.socket
}

setup_fake_store(){
    local top_dir=$1
    mkdir -p $top_dir/asserts
    # debugging
    systemctl status fakestore || true
    echo "Given a controlled store service is up"
    systemd-run --unit fakestore $(which fakestore) -start -dir $top_dir -addr localhost:11028

    echo "And snapd is configured to use the controlled store"
    _configure_store_backends "SNAPPY_FORCE_CPI_URL=http://localhost:11028" "SNAPPY_FORCE_SAS_URL=http://localhost:11028"
}

setup_staging_store(){
    echo "Given ubuntu-core snap is available before switching to staging"
    if ! snap list | grep -q ubuntu-core; then
        snap install ubuntu-core
    fi

    echo "And snapd is configured to use the staging store"
    _configure_store_backends "SNAPPY_USE_STAGING_CPI=1" "SNAPPY_USE_STAGING_SAS=1"
}

teardown_store(){
    local store_type=$1
    local top_dir=$2
    if [ "$store_type" = "fake" ]; then
        systemctl stop fakestore
    fi

    systemctl stop snapd.socket
    rm -rf $STORE_CONFIG $top_dir
    systemctl daemon-reload
    systemctl start snapd.socket
}

setup_store(){
    local store_type=$1
    local top_dir=$2
    if [ "$store_type" = "fake" ]; then
        setup_fake_store $top_dir
    else
        if [ "$store_type" = "staging" ]; then
            setup_staging_store
        fi
        echo "Given a refreshable snap is installed"
        snap install test-snapd-tools
    fi
}
