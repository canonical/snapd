#!/usr/bin/python3

import contextlib
import os
import fcntl
import re
import struct
import sys
from typing import Iterable


def if_open(dev: str) -> int:
    TUNSETIFF = 0x400454ca
    TUNSETOWNER = TUNSETIFF + 2
    IFF_TUN = 0x0001
    IFF_TAP = 0x0002
    IFF_NO_PI = 0x1000

    try:
        fd = os.open("/dev/net/tun", os.O_RDWR)
    except PermissionError as e:
        raise SystemExit(e)

    if_flags = None
    if dev.startswith('tun'):
        if_flags = IFF_TUN | IFF_NO_PI
    elif dev.startswith('tap'):
        if_flags = IFF_TAP | IFF_NO_PI

    fcntl.ioctl(fd, TUNSETIFF, struct.pack("16sH", str.encode(dev), if_flags))

    # for setting to owner
    # fcntl.ioctl(fd, TUNSETOWNER, 1000)

    return fd


def device_exists(dev: str) -> bool:
    return os.path.exists("/sys/devices/virtual/net/%s" % dev)


def valid_device_name(dev: str) -> None:
    if not re.search(r'^t(ap|un)[0-9]+$', dev):
        raise ValueError("device should be of form tun0-tun255 or tap0-tap255")

    if_num = int(dev[3:])
    if if_num > 255:
        raise ValueError("device should be of form tun0-tun255 or tap0-tap255")


@contextlib.contextmanager
def closing_fd(fd: int) -> Iterable[int]:
    try:
        yield fd
    finally:
        os.close(fd)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("need to specify a tun/tap device (eg tun1 or tap2)")

    d = sys.argv[1]
    valid_device_name(d)

    if device_exists(d):
        raise SystemExit("ERROR: device '%s' already exists" % d)
        sys.exit(1)

    found = False
    with closing_fd(if_open(d)) as fd:
        if device_exists(d):
            found = True
            print("PASS")
        else:
            print("FAIL")

    if not found:
        sys.exit(1)
