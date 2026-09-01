#!/bin/bash

build_ubuntu_image() {
    local ubuntu_image_dir
    ubuntu_image_dir="$(mktemp -d "${TMPDIR:-/tmp}/ubuntu-image.XXXXXXXX")"

    git clone --depth=1 https://github.com/canonical/ubuntu-image "$ubuntu_image_dir"
    (
        # go 1.22 needs an exact toolchain name for automatic toolchain selection
        export GOTOOLCHAIN=go1.25.0
        cd "$ubuntu_image_dir" || exit 1
        go mod edit -replace="github.com/snapcore/snapd=$PROJECT_PATH"
        go build -mod=mod -tags withtestkeys -o "$GOHOME/bin/ubuntu-image" ./cmd/ubuntu-image
    )
    rm -rf "$ubuntu_image_dir"
}

get_ubuntu_image() {
    if os.query is-arm; then
        wget -q -c -O ubuntu-image-withtestkeys.tar.gz \
            https://storage.googleapis.com/snapd-spread-tests/ubuntu-image/ubuntu-image-withtestkeys-arm64.tar.gz
    else
        wget -q -c https://storage.googleapis.com/snapd-spread-tests/ubuntu-image/ubuntu-image-withtestkeys.tar.gz
    fi

    tar xvzf ubuntu-image-withtestkeys.tar.gz
    rm -f ubuntu-image-withtestkeys.tar.gz

    test -x ubuntu-image
    mv ubuntu-image "$GOHOME/bin"
}

prepare_ubuntu_image() {
    if [ "${UBUNTU_IMAGE_USE_LOCAL_SNAPD:-false}" = "true" ]; then
        build_ubuntu_image
    else
        get_ubuntu_image
    fi
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
        ubuntu-24.04-64*)
            echo "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
            ;;
        ubuntu-24.04-arm-64*)
            echo "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img"
            ;;
        ubuntu-26.04-64*)
            echo "https://cloud-images.ubuntu.com/resolute/current/resolute-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

# shellcheck disable=SC2120
get_image_url_for_vm() {
    get_ubuntu_image_url_for_vm "$@"
}
