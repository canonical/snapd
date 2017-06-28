#!/bin/sh
setup_ramdisk(){
    if [ ! -e /dev/ram0 ]; then
        mknod -m 660 /dev/ram0 b 1 0
        chown root.disk /dev/ram0
        # wait for all udev events to be handled, sometimes we are getting an error:
        #
        # $ dmsetup -v --noudevsync --noudevrules create dm-ram0 --table '0 131072 linear /dev/ram0 0'
        # device-mapper: reload ioctl on dm-ram0 failed: Device or resource busy
        #
        # and in syslog:
        #
        # Jun 28 09:18:34 localhost kernel: [   36.434220] device-mapper: table: 252:0: linear: Device lookup failed
        # Jun 28 09:18:34 localhost kernel: [   36.434686] device-mapper: ioctl: error adding target to table
        udevadm settle
    fi
}
