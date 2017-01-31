#!/bin/sh

get_common_syscalls() {
    cat <<EOF
# filter that works ok for true

open
close

# for writing to stderr
write

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
getuid32
setresuid
setresuid32
setgid
setgid32
setuid
setuid32

set_thread_area

# for mknod
chmod
getrlimit
ugetrlimit
rt_sigaction
rt_sigprocmask
set_robust_list
set_tid_address
statfs
statfs64
umask
EOF
}

L="$(pwd)/snap-confine/snap-confine"
export L

TMP="$(mktemp -d)"
SHM="$(mktemp -d -p /run/shm)"
trap 'rm -rf $TMP $SHM' EXIT

export SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR="$TMP"
export SNAPPY_LAUNCHER_INSIDE_TESTS="1"
export SNAP_CONFINE_NO_ROOT=1
export SNAP_NAME=name.app

FAIL() {
    printf ": FAIL\n"
    tailcmd="dmesg | tail -10"
    # kern.log has nice timestamps so use it when it is available
    if [ -f /var/log/kern.log ]; then
        tailcmd="tail -10 /var/log/kern.log"
    fi
    printf "Seccomp:\n"
    $tailcmd | grep -F type=1326
    printf "Time: "
    date
    exit 1
}

PASS() {
    printf ": PASS\n"
}
