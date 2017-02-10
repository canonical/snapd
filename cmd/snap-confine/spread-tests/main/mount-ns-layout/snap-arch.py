#!/usr/bin/env python3
import os
import sys

def main():
    kernel_arch = os.uname().machine
    # Because off by one bugs and naming ...
    snap_arch_map = {
        'aarch64': 'arm64',
        'armv7l': 'armhf',
        'x86_64': 'amd64',
        'i686': 'i386',
    }
    try:
        print(snap_arch_map[kernel_arch])
    except KeyError:
        print("unsupported kernel architecture: {!a}".format(kernel_arch), file=sys.stderr)
        return 1


if __name__ == '__main__':
    main()
