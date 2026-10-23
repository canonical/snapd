#!/bin/bash

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
