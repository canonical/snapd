# go-udev

Simple udev implementation in Golang developped from scratch.
This library allow to listen and manage Linux-kernel (since version 2.6.10) Netlink messages to user space (ie: NETLINK_KOBJECT_UEVENT).

Like [`udev`](https://en.wikipedia.org/wiki/Udev) you will be able to monitor and manage devices plug to the system.

## How to

- Get code : `go get github.com/pilebones/go-udev` or `git clone https://github.com/pilebones/go-udev.git`
- Monitor hot-(un)plug devices : 
```
cd go-udev
go build
./go-udev
```

## Throubleshooting

Don't hesitate to notice if you detect a problem with this tool or library.

## Links

- Netlink Manual: http://man7.org/linux/man-pages/man7/netlink.7.html
- Linux source code about: 
  * Struct sockaddr_netlink: http://elixir.free-electrons.com/linux/v3.12/source/lib/kobject_uevent.c#L45
  * KObject action: http://elixir.free-electrons.com/linux/v3.12/source/lib/kobject_uevent.c#L45
