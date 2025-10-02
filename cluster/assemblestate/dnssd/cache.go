package dnssd

import (
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// Cache stores services in memory.
type Cache struct {
	services map[string]*Service
}

// NewCache returns a new in-memory cache.
func NewCache() *Cache {
	return &Cache{
		services: make(map[string]*Service),
	}
}

// Services returns a list of stored services.
func (c *Cache) Services() []*Service {
	tmp := []*Service{}
	for _, s := range c.services {
		tmp = append(tmp, s)
	}
	return tmp
}

// UpdateFrom updates the cache from resource records in msg.
// TODO consider the cache-flush bit to make records as to be deleted in one second
func (c *Cache) UpdateFrom(req *Request) (adds []*Service, rmvs []*Service) {
	answers := filterRecords(req, nil)
	sort.Sort(byType(answers))

	for _, answer := range answers {
		switch rr := answer.(type) {
		case *dns.PTR:
			ttl := time.Duration(rr.Hdr.Ttl) * time.Second

			var entry *Service
			if e, ok := c.services[rr.Ptr]; !ok {
				if ttl == 0 {
					// Ignore new records with no ttl
					break
				}
				entry = newService(rr.Ptr)
				adds = append(adds, entry)
				c.services[entry.EscapedServiceInstanceName()] = entry
			} else {
				entry = e
			}

			entry.TTL = ttl
			entry.expiration = time.Now().Add(ttl)

		case *dns.SRV:
			ttl := time.Duration(rr.Hdr.Ttl) * time.Second
			var entry *Service
			if e, ok := c.services[rr.Hdr.Name]; !ok {
				if ttl == 0 {
					// Ignore new records with no ttl
					break
				}
				entry = newService(rr.Hdr.Name)
				adds = append(adds, entry)
				c.services[entry.EscapedServiceInstanceName()] = entry
			} else {
				entry = e
			}

			entry.SetHostname(rr.Target)
			entry.TTL = ttl
			entry.expiration = time.Now().Add(ttl)
			entry.Port = int(rr.Port)

		case *dns.A:
			for _, entry := range c.services {
				if entry.Hostname() == rr.Hdr.Name {
					entry.addIP(rr.A, req.iface)
				}
			}

		case *dns.AAAA:
			for _, entry := range c.services {
				if entry.Hostname() == rr.Hdr.Name {
					entry.addIP(rr.AAAA, req.iface)
				}
			}

		case *dns.TXT:
			if entry, ok := c.services[rr.Hdr.Name]; ok {
				text := make(map[string]string)
				for _, txt := range rr.Txt {
					elems := strings.SplitN(txt, "=", 2)
					if len(elems) == 2 {
						key := elems[0]
						value := elems[1]

						// Don't override existing keys
						// TODO make txt records case insensitive
						if _, ok := text[key]; !ok {
							text[key] = value
						}

						text[key] = value
					}
				}

				entry.Text = text
				entry.TTL = time.Duration(rr.Hdr.Ttl) * time.Second
				entry.expiration = time.Now().Add(entry.TTL)
			}
		default:
			// ignore
		}
	}

	// TODO remove outdated services regularly
	rmvs = c.removeExpired()

	return
}

func (c *Cache) removeExpired() []*Service {
	var outdated []*Service
	var services = c.services
	for key, srv := range services {
		if time.Now().After(srv.expiration) {
			outdated = append(outdated, srv)
			delete(c.services, key)
		}
	}

	return outdated
}

type byType []dns.RR

func (a byType) Len() int      { return len(a) }
func (a byType) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byType) Less(i, j int) bool {
	// Sort in the following order
	// 1. SRV or PTR
	// 2. Anything else
	switch a[i].(type) {
	case *dns.SRV:
		return true
	case *dns.PTR:
		return true
	}

	return false
}

// filterRecords returns
// - A and AAAA records for the same hostname as in defined by the service
// - SRV records related to the same service instance name
func filterRecords(req *Request, service *Service) []dns.RR {
	if req.iface != nil && service != nil && len(service.Ifaces) > 0 {
		if !service.IsVisibleAtInterface(req.iface.Name) {
			// Ignore records if the request coming from an ignored interface.
			return []dns.RR{}
		}
	}

	var all []dns.RR
	all = append(all, req.msg.Answer...)
	all = append(all, req.msg.Ns...)
	all = append(all, req.msg.Extra...)

	if service == nil {
		return all
	}

	var answers []dns.RR
	for _, answer := range all {
		switch rr := answer.(type) {
		case *dns.SRV:
			if rr.Target == service.Hostname() {
				// Ignore records coming from ourself
				continue
			}
			if rr.Hdr.Name != service.EscapedServiceInstanceName() {
				// Ignore records from other service instances
				continue
			}
		case *dns.A:
			if rr.Hdr.Name != service.Hostname() {
				// Ignore IPv4 address from other hosts
				continue
			}

			ip := rr.A.To4()
			if service.HasIPOnAnyInterface(ip) {
				// Ignore this record because we know that the service
				// has this ip address but on a different interface.
				continue
			}

		case *dns.AAAA:
			if rr.Hdr.Name != service.Hostname() {
				// Ignore IPv6 address from other hosts
				continue
			}

			ip := rr.AAAA.To16()
			if service.HasIPOnAnyInterface(ip) {
				// Ignore this record because we know that the service
				// has this ip address but on a different interface.
				continue
			}
		}
		answers = append(answers, answer)
	}

	return answers
}
