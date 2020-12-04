#!/bin/bash

STORE_CONFIG=/etc/systemd/system/snapd.service.d/store.conf

# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB/systemd.sh"

_configure_store_backends(){
    systemctl stop snapd.service snapd.socket
    mkdir -p "$(dirname $STORE_CONFIG)"
    rm -f "$STORE_CONFIG"
    cat > "$STORE_CONFIG" <<EOF
[Service]
Environment=SNAPD_DEBUG=1 SNAPD_DEBUG_HTTP=7 SNAPPY_TESTING=1
Environment=$*
EOF
    systemctl daemon-reload
    systemctl start snapd.socket
}

setup_staging_store(){
    _configure_store_backends "SNAPPY_USE_STAGING_STORE=1"
}

teardown_staging_store(){
    systemctl stop snapd.service snapd.socket
    rm -rf "$STORE_CONFIG"
    systemctl daemon-reload
    systemctl start snapd.socket
}

init_fake_refreshes(){
    local dir="$1"
    shift

    fakestore make-refreshable --dir "$dir" "$@"
}

make_snap_installable(){
    local dir="$1"
    local snap_path="$2"

    new_snap_declaration "$dir" "$snap_path"
    new_snap_revision "$dir" "$snap_path"
}

make_snap_installable_with_id(){
    local dir="$1"
    local snap_path="$2"
    local snap_id="$3"

    if ! command -v yaml2json; then
        snap install remarshal
    fi
    if ! command -v jq; then
        #shellcheck source=tests/lib/core-config.sh
        . "$TESTSLIB"/core-config.sh

        SUFFIX="$(get_test_snap_suffix)"
        snap install "jq$SUFFIX"
    fi

    # unsquash the snap to get it's name
    unsquashfs -d /tmp/snap-squashfs "$snap_path" meta/snap.yaml
    snap_name=$(yaml2json < /tmp/snap-squashfs/meta/snap.yaml | jq -r .name)
    rm -rf /tmp/snap-squashfs


    cat >> /tmp/snap-decl.json << EOF
{
    "type": "snap-declaration",
    "snap-id": "${snap_id}",
    "publisher-id": "developer1",
    "snap-name": "${snap_name}"
}
EOF

    cat >> /tmp/snap-rev.json << EOF
{
    "type": "snap-revision",
    "snap-id": "${snap_id}"
}
EOF

    fakestore new-snap-declaration --dir "$dir" --snap-decl-json=/tmp/snap-decl.json "$snap_path"
    fakestore new-snap-revision --dir "$dir" --snap-rev-json=/tmp/snap-rev.json "$snap_path"

    rm -rf /tmp/snap-decl.json
    rm -rf /tmp/snap-rev.json
}

new_snap_declaration(){
    local dir="$1"
    local snap_path="$2"
    shift 2

    cp -a "$snap_path" "$dir"
    p=$(fakestore new-snap-declaration --dir "$dir" "$@" "${snap_path}" )
    snap ack "$p"
}

new_snap_revision(){
    local dir="$1"
    local snap_path="$2"
    shift 2

    p=$(fakestore new-snap-revision --dir "$dir" "$@" "${snap_path}")
    snap ack "$p"
}


setup_fake_store(){
    # before switching make sure we have a session macaroon
    snap find test-snapd-tools

    local top_dir=$1

    mkdir -p "$top_dir/asserts"
    # debugging
    systemctl status fakestore || true
    echo "Given a controlled store service is up"

    https_proxy=${https_proxy:-}
    http_proxy=${http_proxy:-}

    echo "Create fakestore at the given port"
    PORT="11028"
    systemd-run --unit fakestore --setenv SNAPD_DEBUG=1 --setenv SNAPD_DEBUG_HTTP=7 --setenv SNAPPY_TESTING=1 --setenv SNAPPY_USE_STAGING_STORE="$SNAPPY_USE_STAGING_STORE" fakestore run --dir "$top_dir" --addr "localhost:$PORT" --https-proxy="${https_proxy}" --http-proxy="${http_proxy}" --assert-fallback

    echo "And snapd is configured to use the controlled store"
    _configure_store_backends "SNAPPY_FORCE_API_URL=http://localhost:$PORT" "SNAPPY_USE_STAGING_STORE=$SNAPPY_USE_STAGING_STORE"

    echo "Wait until fake store is ready"
    # shellcheck source=tests/lib/network.sh
    . "$TESTSLIB"/network.sh
    if wait_listen_port "$PORT"; then
        return 0
    fi

    echo "fakestore service not started properly"
    ss -ntlp | grep "127.0.0.1:$PORT" || true
    "$TESTSTOOLS"/journal-state get-log -u fakestore || true
    systemctl status fakestore || true
    exit 1
}

teardown_fake_store(){
    local top_dir=$1
    systemctl stop fakestore || true
    # when a unit fails, systemd may keep its status, resetting it allows to
    # start the unit again with a clean slate
    systemctl reset-failed fakestore || true

    if [ "$REMOTE_STORE" = "staging" ]; then
        setup_staging_store
    else
        systemctl stop snapd.service snapd.socket
        rm -rf "$STORE_CONFIG" "$top_dir"
        systemctl daemon-reload
        systemctl start snapd.socket
    fi
}
