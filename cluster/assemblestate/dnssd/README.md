# DNS-SD

[![Build Status](https://travis-ci.org/brutella/hc.svg)](https://travis-ci.org/brutella/dnssd)

This library implements [Multicast DNS][mdns] and [DNS-Based Service Discovery][dnssd] to provide zero-configuration operations. It lets you announce and find services in a specific link-local domain.

[mdns]: https://tools.ietf.org/html/rfc6762
[dnssd]: https://tools.ietf.org/html/rfc6763

## Usage

#### Create a mDNS responder

The following code creates a service with name "My Website._http._tcp.local." for the host "My Computer" which has all IPs from network interface "eth0". The service is added to a responder.

```go
import (
	"context"
	"github.com/brutella/dnssd"
)

cfg := dnssd.Config{
    Name:   "My Website",
    Type:   "_http._tcp",
    Domain: "local",
    Host:   "My Computer",
    Ifaces: []string{"eth0"},,
    Port:   12345,
}
sv, _ := dnssd.NewService(cfg)
```

In most cases you only need to specify the name, type and port of the service.

```go
cfg := dnssd.Config{
    Name:   "My Website",
    Type:   "_http._tcp",
    Port:   12345,
}
sv, _ := dnssd.NewService(cfg)
```

Then you create a responder and add the service to it.
```go
rp, _ := dnssd.NewResponder()
hdl, _ := rp.Add(sv)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

rp.Respond(ctx)
```

When calling `Respond` the responder probes for the service instance name and host name to be unqiue in the network. 
Once probing is finished, the service will be announced.

#### Update TXT records

Once a service is added to a responder, you can use the `hdl` to update properties.

```go
hdl.UpdateText(map[string]string{"key1": "value1", "key2": "value2"}, rsp)
```

## `dnssd` command

The command line tool in `cmd/dnssd` lets you browse, register and resolve services similar to [dns-sd](https://www.unix.com/man-page/osx/1/dns-sd/).

### Install
You can install the tool with

`go install github.com/brutella/dnssd/cmd/dnssd`

### Usage

**Registering a service on your local machine**

Lets register a printer service (`_printer._tcp`) running on your local computer at port 515 with the name "Private Printer".

```sh
dnssd register -Name="Private Printer" -Type="_printer._tcp" -Port=515
```

**Registering a proxy service**

If the service is running on a different machine on your local network, you have to specify the hostname and IP.
Lets say the printer service is running on the printer with the hostname `ABCD` and IPv4 address `192.168.1.53`, you can register a proxy which announce that service on your network.

```sh
dnssd register -Name="Private Printer" -Type="_printer._tcp" -Port=515 -IP=192.168.1.53 -Host=ABCD
```

Use option `-Interface`, if you want to announce the service only on a specific network interface.
This might be necessary if your local machine is connected to multiple subnets and your announced service is only available on a specific subnet.

```sh
dnssd register -Name="Private Printer" -Type="_printer._tcp" -Port=515 -IP=192.168.1.53 -Host=ABCD -Interface=en0
```

**Browsing for a service**

If you want to browse for a service type, you can use the `browse` command.

```sh
dnssd browse -Type="_printer._tcp"
```

**Resolving a service instance**

If you know the name of a service instance, you can resolve its hostname with the `resolve` command.

```sh
dnssd resolve -Name="Private Printer" -Type="_printer._tcp"
```

## Conformance

This library passes the [multicast DNS tests](https://github.com/brutella/dnssd/blob/36a2d8c541aab14895fc5492d5ad8ec447a67c47/_cmd/bct/ConformanceTestResults) of Apple's Bonjour Conformance Test.

## TODO

- [ ] Support hot plugging
- [ ] Support negative responses (RFC6762 6.1)
- [ ] Handle txt records case insensitive
- [ ] Remove outdated services from cache regularly
- [ ] Make sure that hostnames are FQDNs

# Contact

Matthias Hochgatterer

Github: [https://github.com/brutella](https://github.com/brutella/)

Twitter: [https://twitter.com/brutella](https://twitter.com/brutella)


# License

*dnssd* is available under the MIT license. See the LICENSE file for more info.
