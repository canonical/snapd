package dnssd

import (
	"fmt"
	"net"
	"reflect"
	"sort"

	"github.com/miekg/dns"
)

// PTR returns the PTR record for the service.
func PTR(srv Service) *dns.PTR {
	return &dns.PTR{
		Hdr: dns.RR_Header{
			Name:   srv.ServiceName(),
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    TTLDefault,
		},
		Ptr: srv.EscapedServiceInstanceName(),
	}
}

func DNSSDServicesPTR(srv Service) *dns.PTR {
	return &dns.PTR{
		Hdr: dns.RR_Header{
			Name:   srv.ServicesMetaQueryName(),
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    TTLDefault,
		},
		Ptr: srv.ServiceName(),
	}
}

// SRV returns the SRV record for the service.
func SRV(srv Service) *dns.SRV {
	return &dns.SRV{
		Hdr: dns.RR_Header{
			Name:   srv.EscapedServiceInstanceName(),
			Rrtype: dns.TypeSRV,
			Class:  dns.ClassINET,
			Ttl:    TTLHostname,
		},
		Priority: 0,
		Weight:   0,
		Port:     uint16(srv.Port),
		Target:   srv.Hostname(),
	}
}

// TXT returns the TXT record for the service.
func TXT(srv Service) *dns.TXT {
	keys := []string{}
	for key := range srv.Text {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	txts := []string{}
	for _, k := range keys {
		txts = append(txts, fmt.Sprintf("%s=%s", k, srv.Text[k]))
	}

	// An empty TXT record containing zero strings is not allowed. (RFC6763 6.1)
	if len(txts) == 0 {
		txts = []string{""}
	}

	return &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   srv.EscapedServiceInstanceName(),
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    TTLDefault,
		},
		Txt: txts,
	}
}

// NSEC returns the NSEC record for the service.
func NSEC(rr dns.RR, srv Service, iface *net.Interface) *dns.NSEC {
	if iface != nil && !srv.IsVisibleAtInterface(iface.Name) {
		return nil
	}

	switch r := rr.(type) {
	case *dns.PTR:
		return &dns.NSEC{
			Hdr: dns.RR_Header{
				Name:   r.Ptr,
				Rrtype: dns.TypeNSEC,
				Class:  dns.ClassINET,
				Ttl:    TTLDefault,
			},
			NextDomain: r.Ptr,
			TypeBitMap: []uint16{dns.TypeTXT, dns.TypeSRV},
		}
	case *dns.SRV:
		types := []uint16{}
		ips := srv.IPsAtInterface(iface)
		if includesIPv4(ips) {
			types = append(types, dns.TypeA)
		}
		if includesIPv6(ips) {
			types = append(types, dns.TypeAAAA)
		}

		if len(types) > 0 {
			return &dns.NSEC{
				Hdr: dns.RR_Header{
					Name:   r.Target,
					Rrtype: dns.TypeNSEC,
					Class:  dns.ClassINET,
					Ttl:    TTLDefault,
				},
				NextDomain: r.Target,
				TypeBitMap: types,
			}
		}
	default:
	}

	return nil
}

// A returns the A records (IPv4 addresses) for the service.
func A(srv Service, iface *net.Interface) []*dns.A {
	if iface == nil {
		return []*dns.A{}
	}

	if !srv.IsVisibleAtInterface(iface.Name) {
		return []*dns.A{}
	}

	ips := srv.IPsAtInterface(iface)

	var as []*dns.A
	for _, ip := range ips {
		if ip.To4() != nil {
			a := &dns.A{
				Hdr: dns.RR_Header{
					Name:   srv.Hostname(),
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    TTLHostname,
				},
				A: ip,
			}
			as = append(as, a)
		}
	}

	return as
}

// AAAA returns the AAAA records (IPv6 addresses) of the service.
func AAAA(srv Service, iface *net.Interface) []*dns.AAAA {
	if iface == nil {
		return []*dns.AAAA{}
	}

	if !srv.IsVisibleAtInterface(iface.Name) {
		return []*dns.AAAA{}
	}

	ips := srv.IPsAtInterface(iface)

	var aaaas []*dns.AAAA
	for _, ip := range ips {
		if ip.To4() == nil && ip.To16() != nil {
			aaaa := &dns.AAAA{
				Hdr: dns.RR_Header{
					Name:   srv.Hostname(),
					Rrtype: dns.TypeAAAA,
					Class:  dns.ClassINET,
					Ttl:    TTLHostname,
				},
				AAAA: ip,
			}
			aaaas = append(aaaas, aaaa)
		}
	}

	return aaaas
}

func splitRecords(records []dns.RR) (as []*dns.A, aaaas []*dns.AAAA, srvs []*dns.SRV) {
	for _, record := range records {
		switch rr := record.(type) {
		case *dns.A:
			if rr.A.To4() != nil {
				as = append(as, rr)
			}

		case *dns.AAAA:
			if rr.AAAA.To16() != nil {
				aaaas = append(aaaas, rr)
			}
		case *dns.SRV:
			srvs = append(srvs, rr)
		}
	}
	return
}

// Returns true if ips contains IPv4 addresses.
func includesIPv4(ips []net.IP) bool {
	for _, ip := range ips {
		if ip.To4() != nil {
			return true
		}
	}

	return false
}

// Returns true if ips contains IPv6 addresses.
func includesIPv6(ips []net.IP) bool {
	for _, ip := range ips {
		if ip.To4() == nil && ip.To16() != nil {
			return true
		}
	}

	return false
}

// Removes this from that.
func remove(this []dns.RR, that []dns.RR) []dns.RR {
	var result []dns.RR
	for _, thatRr := range that {
		isUnknown := true
		for _, thisRr := range this {
			switch a := thisRr.(type) {
			case *dns.PTR:
				if ptr, ok := thatRr.(*dns.PTR); ok {
					if a.Ptr == ptr.Ptr && a.Hdr.Name == ptr.Hdr.Name && a.Hdr.Ttl > ptr.Hdr.Ttl/2 {
						isUnknown = false
					}
				}
			case *dns.SRV:
				if srv, ok := thatRr.(*dns.SRV); ok {
					if a.Target == srv.Target && a.Port == srv.Port && a.Hdr.Name == srv.Hdr.Name && a.Hdr.Ttl > srv.Hdr.Ttl/2 {
						isUnknown = false
					}
				}
			case *dns.TXT:
				if txt, ok := thatRr.(*dns.TXT); ok {
					if reflect.DeepEqual(a.Txt, txt.Txt) && a.Hdr.Ttl > txt.Hdr.Ttl/2 {
						isUnknown = false
					}
				}
			}
		}

		if isUnknown {
			result = append(result, thatRr)
		}
	}

	return result
}

// mergeMsgs merges the records in msgs into one message.
func mergeMsgs(msgs []*dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.Answer = []dns.RR{}
	resp.Ns = []dns.RR{}
	resp.Extra = []dns.RR{}
	resp.Question = []dns.Question{}

	for _, msg := range msgs {
		if msg.Answer != nil {
			resp.Answer = append(resp.Answer, remove(resp.Answer, msg.Answer)...)
		}
		if msg.Ns != nil {
			resp.Ns = append(resp.Ns, remove(resp.Ns, msg.Ns)...)
		}
		if msg.Extra != nil {
			resp.Extra = append(resp.Extra, remove(resp.Extra, msg.Extra)...)
		}

		if msg.Question != nil {
			resp.Question = append(resp.Question, msg.Question...)
		}
	}

	return resp
}
