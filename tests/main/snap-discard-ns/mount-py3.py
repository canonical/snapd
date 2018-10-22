#!/usr/bin/env python3
"""
Pure-python limited-use wrapper around the mount library function.
"""
from argparse import ArgumentParser
from ctypes import CDLL, c_char_p, c_long, get_errno
from ctypes.util import find_library
from enum import IntEnum
from os import strerror
from sys import stderr

__all__ = ('mount', )


class MountOpts(IntEnum):
    """MountOpts contain various flags for the mount system call."""

    Bind = 4096


def mount(source: str, target: str, fstype: str, flags: int = 0,
          data: str = "") -> None:
    """mount is a thin wrapper around the mount library function."""
    libc_name = find_library("c")
    if libc_name is None:
        raise Exception("cannot find the C library")
    libc = CDLL(libc_name, use_errno=True)
    retval = libc.mount(
            c_char_p(source.encode("UTF-8")),
            c_char_p(target.encode("UTF-8")),
            c_char_p(fstype.encode("UTF-8")),
            c_long(flags),
            None if data == "" else c_char_p(data.encode("UTF-8")))
    if retval < 0:
        errno = get_errno()
        raise OSError(errno, strerror(errno))


def main() -> None:
    parser = ArgumentParser()
    parser.add_argument("source", help="path of the device or bind source")
    parser.add_argument("target", help="path of the new mount point")
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--bind", action="store_const", const="bind",
                       help="create a new mount point of existing path")
    group.add_argument("-t", "--type", help="filesystem type to mount")
    opts = parser.parse_args()
    if opts.bind is not None:
        mount(opts.source, opts.target, "", MountOpts.Bind)
    else:
        mount(opts.source, opts.target, opts.fstype)


if __name__ == "__main__":
    try:
        main()
    except Exception as err:
        print(err, file=stderr)
        raise SystemExit(1)
