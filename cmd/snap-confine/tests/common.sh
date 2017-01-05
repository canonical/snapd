#!/bin/sh

get_common_syscalls() {
    cat <<EOF
# syscalls required by snap-confine to finish working
setresuid
setgid
setuid
geteuid32

# filter that works ok for true
open
close

mmap
mmap2
munmap
mprotect

fstat
fstat64
access
read

brk
execve

arch_prctl
exit_group

geteuid
geteuid32
getuid

set_thread_area
EOF
}

L="$(pwd)/../snap-confine"
export L

TMP="$(mktemp -d)"
trap 'rm -rf $TMP' EXIT

export SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR="$TMP"
export SNAPPY_LAUNCHER_INSIDE_TESTS="1"
export SNAP_CONFINE_NO_ROOT=1
export SNAP_NAME=name.app

FAIL() {
    printf ": FAIL\n"
    exit 1
}

PASS() {
    printf ": PASS\n"
}
