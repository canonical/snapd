#!/usr/bin/python3

import os
import fcntl
import re
import struct
import sys
import time


def if_open(dev):
    TUNSETIFF = 0x400454ca
    TUNSETOWNER = TUNSETIFF + 2
    IFF_TUN = 0x0001
    IFF_TAP = 0x0002
    IFF_NO_PI = 0x1000

    try:
        fd = os.open("/dev/net/tun", os.O_RDWR)
    except PermissionError as e:
        print("%s" % e)
        sys.exit(1)
    except Exception:
        raise

    if_flags = None
    if dev.startswith('tun'):
        if_flags = IFF_TUN | IFF_NO_PI
    elif dev.startswith('tap'):
        if_flags = IFF_TAP | IFF_NO_PI

    iface = fcntl.ioctl(fd, TUNSETIFF,
                        struct.pack("16sH", str.encode(dev), if_flags))

    # for setting to owner
    # fcntl.ioctl(fd, TUNSETOWNER, 1000)

    return fd


def device_exists(dev):
    return os.path.exists("/sys/devices/virtual/net/%s" % dev)


def valid_device_name(dev):
    if not re.search(r'^t(ap|un)[0-9]+$', dev):
        raise Exception("device should be of form tun0-tun255 or tap0-tap255")

    if_num = int(dev[3:])
    if if_num > 255:
        raise Exception("device should be of form tun0-tun255 or tap0-tap255")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("need to specify a tun/tap device (eg tun1 or tap2)")
        sys.exit(1)

    d = sys.argv[1]
    valid_device_name(d)

    if device_exists(d):
        print("ERROR: device '%s' already exists" % d)
        sys.exit(1)

    fd = if_open(d)

    found = False
    if device_exists(d):
        found = True
        print("PASS")
    else:
        print("FAIL")

    os.close(fd)

    if not found:
        sys.exit(1)
