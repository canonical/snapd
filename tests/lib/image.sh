#!/bin/bash

build_ubuntu_image() {
    # the helper can be invoked multiple times, so do nothing if ubuntu-image
    # was already built and is accessible
    if command -v ubuntu-image; then
        return
    fi

    if [ "${UBUNTU_IMAGE_ALLOW_API_BREAK-true}" = "true" ]; then
        (
            # build a version which uses the current snapd tree as a dependency
            # shellcheck disable=SC2030,SC2031
            export GO111MODULE=off
            # use go get so that ubuntu-image is built with current snapd
            # sources
            go get github.com/canonical/ubuntu-image/cmd/ubuntu-image
            go install -tags 'withtestkeys' github.com/canonical/ubuntu-image/cmd/ubuntu-image@9196729ef8f14f530bec2c7512adea07e8f74da5
        )
    else
        (
            # build a version which uses a particular revision of snapd listed
            # in ubuntu-image go.mod file
            # shellcheck disable=SC2030,SC2031
            export GO111MODULE=on
            # shellcheck disable=SC2030,SC2031
            unset GOPATH
            cd /tmp || exit 1
            git clone https://github.com/canonical/ubuntu-image
            cd ubuntu-image || exit 1
            git checkout 9196729ef8f14f530bec2c7512adea07e8f74da5 || exit 1
            go build -tags 'withtestkeys' ./cmd/ubuntu-image
        )
        # make it available
        mv /tmp/ubuntu-image/ubuntu-image "$GOHOME/bin"
    fi
}


get_ubuntu_image() {
    wget -c https://storage.googleapis.com/snapd-spread-tests/ubuntu-image/ubuntu-image-withtestkeys.tar.gz
    tar xvzf ubuntu-image-withtestkeys.tar.gz
    rm -f ubuntu-image-withtestkeys.tar.gz

    test -x ubuntu-image
    mv ubuntu-image "$GOHOME/bin"
}

# shellcheck disable=SC2120
get_google_image_url_for_vm() {
    case "${1:-$SPREAD_SYSTEM}" in
        ubuntu-16.04-64*)
            echo "https://storage.googleapis.com/snapd-spread-tests/images/cloudimg/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64*)
            echo "https://storage.googleapis.com/snapd-spread-tests/images/cloudimg/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64*)
            echo "https://storage.googleapis.com/snapd-spread-tests/images/cloudimg/focal-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-arm-64*)
            echo "https://storage.googleapis.com/snapd-spread-tests/images/cloudimg/focal-server-cloudimg-arm64.img"
            ;;
        ubuntu-22.04-64*)
            echo "https://storage.googleapis.com/snapd-spread-tests/images/cloudimg/jammy-server-cloudimg-amd64.img"
            ;;
        ubuntu-22.04-arm-64*)
            echo "https://storage.googleapis.com/snapd-spread-tests/images/cloudimg/jammy-server-cloudimg-arm64.img"
            ;;
        ubuntu-23.10-64*)
            echo "https://storage.googleapis.com/snapd-spread-tests/images/cloudimg/mantic-server-cloudimg-amd64.img"
            ;;
        ubuntu-24.04-64*)
            echo "https://storage.googleapis.com/snapd-spread-tests/images/cloudimg/noble-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
    esac
}

# shellcheck disable=SC2120
get_ubuntu_image_url_for_vm() {
    case "${1:-$SPREAD_SYSTEM}" in
        ubuntu-16.04-64*)
            echo "https://cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64*)
            echo "https://cloud-images.ubuntu.com/bionic/current/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64*)
            echo "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-arm-64*)
            echo "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-arm64.img"
            ;;
        ubuntu-22.04-64*)
            echo "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"
            ;;
        ubuntu-22.04-arm-64*)
            echo "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-arm64.img"
            ;;
        ubuntu-23.10-64*)
            echo "https://cloud-images.ubuntu.com/mantic/current/mantic-server-cloudimg-amd64.img"
            ;;
        ubuntu-24.04-64*)
            echo "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

# shellcheck disable=SC2120
get_image_url_for_vm() {
    if [[ "$SPREAD_BACKEND" == google* ]]; then
        get_google_image_url_for_vm "$@"
    else
        get_ubuntu_image_url_for_vm "$@"
    fi
}
