# go-udev

Simple udev implementation in Golang developped from scratch.
This library allow to listen and manage Linux-kernel (since version 2.6.10) Netlink messages to user space (ie: NETLINK_KOBJECT_UEVENT).

Like [`udev`](https://en.wikipedia.org/wiki/Udev) you will be able to monitor and manage devices plug to the system.

## How to

- Get sources: `go get github.com/pilebones/go-udev` or `git clone https://github.com/pilebones/go-udev.git`
- Execute unit-test: `go test`
- Monitor hot-(un)plug devices:
```
cd go-udev
go build
./go-udev
```

To implement your own monitoring system, please see `main.go` as a simple example.

## Examples

Example of output when a USB storage is plugged:
```
2017/10/20 23:47:23 Handle main.UEvent{
    Action: "add",
    KObj:   "/devices/pci0000:00/0000:00:14.0/usb1/1-1",
    Env:    {"PRODUCT":"58f/6387/10b", "TYPE":"0/0/0", "BUSNUM":"001", "DEVNUM":"005", "SEQNUM":"2511", "DEVNAME":"bus/usb/001/005", "DEVPATH":"/devices/pci0000:00/0000:00:14.0/usb1/1-1", "SUBSYSTEM":"usb", "MAJOR":"189", "MINOR":"4", "DEVTYPE":"usb_device", "ACTION":"add"},
}
2017/10/20 23:47:23 Handle main.UEvent{
    Action: "add",
    KObj:   "/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0",
    Env:    {"SUBSYSTEM":"usb", "TYPE":"0/0/0", "INTERFACE":"8/6/80", "MODALIAS":"usb:v058Fp6387d010Bdc00dsc00dp00ic08isc06ip50in00", "SEQNUM":"2512", "DEVPATH":"/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0", "DEVTYPE":"usb_interface", "PRODUCT":"58f/6387/10b", "ACTION":"add"},
}
2017/10/20 23:47:23 Handle main.UEvent{
    Action: "add",
    KObj:   "/module/usb_storage",
    Env:    {"SEQNUM":"2513", "ACTION":"add", "DEVPATH":"/module/usb_storage", "SUBSYSTEM":"module"},
}
[...]
```

Example of output when a USB storage is unplugged:
```
[...]
2017/10/20 23:47:29 Handle main.UEvent{
    Action: "remove",
    KObj:   "/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0/host4/target4:0:0/4:0:0:0/block/sdb",
    Env:    {"SUBSYSTEM":"block", "MAJOR":"8", "MINOR":"16", "DEVNAME":"sdb", "DEVTYPE":"disk", "SEQNUM":"2543", "ACTION":"remove", "DEVPATH":"/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0/host4/target4:0:0/4:0:0:0/block/sdb"},
}
[...]
2017/10/20 23:47:29 Handle main.UEvent{
    Action: "remove",
    KObj:   "/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0",
    Env:    {"ACTION":"remove", "SUBSYSTEM":"usb", "DEVTYPE":"usb_interface", "SEQNUM":"2548", "MODALIAS":"usb:v058Fp6387d010Bdc00dsc00dp00ic08isc06ip50in00", "DEVPATH":"/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0", "PRODUCT":"58f/6387/10b", "TYPE":"0/0/0", "INTERFACE":"8/6/80"},
}
2017/10/20 23:47:29 Handle main.UEvent{
    Action: "remove",
    KObj:   "/devices/pci0000:00/0000:00:14.0/usb1/1-1",
    Env:    {"PRODUCT":"58f/6387/10b", "TYPE":"0/0/0", "DEVNUM":"005", "SEQNUM":"2549", "ACTION":"remove", "DEVPATH":"/devices/pci0000:00/0000:00:14.0/usb1/1-1", "SUBSYSTEM":"usb", "MAJOR":"189", "MINOR":"4", "DEVNAME":"bus/usb/001/005", "DEVTYPE":"usb_device", "BUSNUM":"001"},
}
```

## Throubleshooting

Don't hesitate to notice if you detect a problem with this tool or library.

## Links

- Netlink Manual: http://man7.org/linux/man-pages/man7/netlink.7.html
- Linux source code about: 
  * Struct sockaddr_netlink: http://elixir.free-electrons.com/linux/v3.12/source/lib/kobject_uevent.c#L45
  * KObject action: http://elixir.free-electrons.com/linux/v3.12/source/lib/kobject_uevent.c#L45
