#!/bin/sh
STORE_CONFIG=/etc/systemd/system/snapd.service.d/store.conf

configure_store_backend(){
    local store_url=$1
    systemctl stop snapd.service snapd.socket
    mkdir -p $(dirname $STORE_CONFIG)
    cat > $STORE_CONFIG <<EOF
[Service]
Environment="SNAPPY_FORCE_CPI_URL=http://$store_url"
EOF
    systemctl daemon-reload
    systemctl start snapd.socket
}

setup_fake_store(){
    local blob_dir=$1
    echo "Given a controlled store service is up"
    systemd-run --unit fakestore $(which fakestore) -start -blobdir $blob_dir -addr localhost:11028

    echo "And a snap is installed"
    snap install test-snapd-tools

    echo "And snapd is configured to use the controlled store"
    configure_store_backend localhost:11028

    echo "And a new version of that snap put in the controlled store"
    fakestore -blobdir $blob_dir -make-refreshable test-snapd-tools
}

setup_staging_store(){
    echo "Given ubuntu-core snap is available before switching to staging"
    snap install hello-world

    echo "And snapd is configured to use the staging store"
    configure_store_backend search.apps.staging.ubuntu.com/api/v1/
}

teardown_store(){
    local store_type=$1
    local blob_dir=$2
    if [ "$store_type" = "fake" ]; then
        systemctl stop fakestore
    fi

    systemctl stop snapd.socket
    rm -rf $STORE_CONFIG $blob_dir
    systemctl daemon-reload
    systemctl start snapd.socket
}

setup_store(){
    local store_type=$1
    local blob_dir=$2
    if [ "$store_type" = "fake" ]; then
        setup_fake_store $blob_dir
    else
        if [ "$store_type" = "staging" ]; then
            setup_staging_store
        fi
        echo "Given a refreshable snap is installed"
        snap install test-snapd-tools
    fi
}
