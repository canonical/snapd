package assemblestate

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/brutella/dnssd"
)

func MulticastDiscovery(
	ctx context.Context,
	iface string,
	address string,
	port int,
	rdt DeviceToken,
) (<-chan []string, func(), error) {
	const service = "_snapd._https"
	sv, err := dnssd.NewService(dnssd.Config{
		Name:   fmt.Sprintf("snapd-%s", rdt),
		Type:   service,
		Port:   port,
		Ifaces: []string{iface},
		IPs:    []net.IP{net.ParseIP(address)},
	})
	if err != nil {
		return nil, nil, err
	}

	rp, err := dnssd.NewResponder()
	if err != nil {
		return nil, nil, err
	}

	_, err = rp.Add(sv)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rp.Respond(ctx)
	}()

	addresses := make(chan []string)

	wg.Add(1)
	go func() {
		defer wg.Done()
		const domain = "local"
		dnssd.LookupType(ctx, fmt.Sprintf("%s.%s.", service, domain), func(be dnssd.BrowseEntry) {
			addrs := make([]string, 0, len(be.IPs))
			for _, ip := range be.IPs {
				// drop non ipv4 for now, just for simplicity
				if len(ip) != net.IPv4len {
					continue
				}

				addrs = append(addrs, fmt.Sprintf("%s:%d", ip, be.Port))
			}
			addresses <- addrs
		}, func(be dnssd.BrowseEntry) {})
	}()

	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		wg.Wait()
	}

	return addresses, stop, nil
}
