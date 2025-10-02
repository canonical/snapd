package dnssd

import (
	"context"
	"net"

	"github.com/brutella/dnssd/log"
	"github.com/vishvananda/netlink"
)

// linkSubscribe subscribes to network interface updates (Ethernet cable is plugged in) via the netlink API.
// For simplicity reasons, all network interfaces are re-announced, when network interfaces change.
func (r *responder) linkSubscribe(ctx context.Context) {
	done := make(chan struct{})
	defer close(done)

	ch := make(chan netlink.LinkUpdate, 1)
	if err := netlink.LinkSubscribe(ch, done); err != nil {
		return
	}

	log.Debug.Println("waiting for link updates...")

	for {
		select {
		case update := <-ch:
			iface, err := net.InterfaceByIndex(int(update.Index))
			if err != nil {
				log.Info.Println(err)
				continue
			}

			if isInterfaceUpAndRunning(iface) {
				log.Debug.Printf("interface %s is up", iface.Name)

				addrs, err := iface.Addrs()
				if err == nil {
					log.Debug.Printf("addrs %+v", addrs)
				}
			} else {
				log.Debug.Printf("interface %s is down", iface.Name)
			}

			log.Debug.Println("announcing services after link update")
			r.mutex.Lock()
			r.announce(services(r.managed))
			r.mutex.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

func isInterfaceUpAndRunning(iface *net.Interface) bool {
	return iface.Flags&net.FlagUp == net.FlagUp && iface.Flags&net.FlagRunning == net.FlagRunning
}
