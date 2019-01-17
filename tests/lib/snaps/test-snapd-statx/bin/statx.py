#!/usr/bin/env python3
"""
Pure-python implementation of the status of the stax() system call.
"""
from ctypes import CDLL, c_char, c_long, create_string_buffer, get_errno
from ctypes.util import find_library
from enum import Enum
from errno import ENOSYS, EPERM
from os import uname


__all__ = ('SyscallStatus', 'evaluate_statx_support')


class SyscallStatus(Enum):
    """Syscall status encodes the status of a system call."""
    SUPPORTED = "supported"
    MISSING = "missing"
    BLOCKED = "blocked"


def evaluate_statx_support() -> SyscallStatus:
    """
    Evaluate status of the statx system call.

    The statx system call was introduced in the Linux kernel 4.11.  A snap
    application running under seccomp confinement may experience one of three
    results when accessing statx: Supported, namely implemented by the kernel
    and allowed by the seccomp filter. Missing, namely not implemented by the
    kernel but not blocked by seccomp. Blocked namely not allowed by seccomp
    and either implemented or not by the kernel.
    """
    machine = uname()[4]
    sys_statx_table = {
        "i686": 383,
        "i386": 383,
        "x86_64": 332,

        "arm": 397,
        "aarch64": 291,

        "ppc": 383,
        "ppcel64": 383,

        "s390x": 379,
    }
    try:
        SYS_STATX = sys_statx_table[machine]
    except KeyError:
        raise Exception("unsupported architecture {}".format(machine))
    libc_name = find_library("c")
    if libc_name is None:
        raise Exception("cannot find the C library")
    libc = CDLL(libc_name, use_errno=True)
    syscall = libc.syscall
    buf_4k = c_char * 4096
    dummy_buf = buf_4k()
    empty_string = create_string_buffer(b"")
    AT_FDCWD = -100
    AT_EMPTY_PATH = 0x1000
    # statx(2) prototype:
    #
    # int statx(int dirfd, const char *pathname, int flags,
    #           unsigned int mask, struct statx *statxbuf);
    #
    # We don't attempt to define struct statx here, instead we just
    # provide a buffer with ample space, so that the system call has sufficient
    # space to write the result to.
    #
    # Python equivalent of:
    #
    #   char dummy_buf[4096];
    #   syscall(SYS_STATX, AT_FDCWD, "", AT_EMPTY_PATH, 0, dummy_buf);
    retval = syscall(c_long(SYS_STATX), c_long(AT_FDCWD), empty_string,
                     c_long(AT_EMPTY_PATH), c_long(0), dummy_buf)
    errno = get_errno()
    if retval == 0:
        return SyscallStatus.SUPPORTED
    elif retval == -1 and errno == ENOSYS:
        return SyscallStatus.MISSING
    elif retval == -1 and errno == EPERM:
        return SyscallStatus.BLOCKED
    raise Exception("unexpected result of statx, retval: {}, errno: {}".format(
        retval, errno))


def main() -> None:
    try:
        print("statx: {}".format(evaluate_statx_support().value))
    except Exception as exc:
        print("statx: error: {}".format(exc))


if __name__ == "__main__":
    main()
