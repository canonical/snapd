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
            go install -tags 'withtestkeys' github.com/canonical/ubuntu-image/cmd/ubuntu-image
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
            go build -tags 'withtestkeys' ./cmd/ubuntu-image
        )
        # make it available
        cp -av /tmp/ubuntu-image/ubuntu-image "$GOHOME/bin"
    fi
}


get_ubuntu_image() {
    wget https://storage.googleapis.com/snapd-spread-tests/ubuntu-image/ubuntu-image-withtestkeys.tar.gz
    tar -xvzf ubuntu-image-withtestkeys-tmp.tar.gz
    test -x ./ubuntu-image
    cp -av ./ubuntu-image "$GOHOME/bin"
}
