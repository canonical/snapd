#!/bin/sh
    
L="$(pwd)/../src/snap-run"
export L

TMP="$(mktemp -d)"
trap 'rm -rf $TMP' EXIT

export SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR="$TMP"
export SNAPPY_LAUNCHER_INSIDE_TESTS="1"
export UBUNTU_CORE_LAUNCHER_NO_ROOT=1

FAIL() {
    printf ": FAIL\n"
    exit 1
}

PASS() {
    printf ": PASS\n"
}
