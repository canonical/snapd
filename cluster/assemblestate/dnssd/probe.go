package dnssd

import (
	"context"
	"math/rand"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/brutella/dnssd/log"
	"github.com/miekg/dns"
)

// ProbeService probes for the hostname and service instance name of srv.
// If err == nil, the returned service is verified to be unique on the local network.
func ProbeService(ctx context.Context, srv Service) (Service, error) {
	conn, err := newMDNSConn(srv.Ifaces...)

	if err != nil {
		return srv, err
	}

	defer conn.close()

	// After one minute of probing, if the Multicast DNS responder has been
	// unable to find any unused name, it should log an error (RFC6762 9)
	probeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// When ready to send its Multicast DNS probe packet(s) the host should
	// first wait for a short random delay time, uniformly distributed in
	// the range 0-250 ms. (RFC6762 8.1)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	delay := time.Duration(r.Intn(250)) * time.Millisecond
	log.Debug.Println("Probing delay", delay)
	time.Sleep(delay)

	return probeService(probeCtx, conn, srv, 250*time.Millisecond, false)
}

func ReprobeService(ctx context.Context, srv Service) (Service, error) {
	conn, err := newMDNSConn(srv.Ifaces...)

	if err != nil {
		return srv, err
	}

	defer conn.close()
	return probeService(ctx, conn, srv, 250*time.Millisecond, true)
}

func probeService(ctx context.Context, conn MDNSConn, srv Service, delay time.Duration, probeOnce bool) (s Service, e error) {
	candidate := srv.Copy()
	prevConflict := probeConflict{}

	// Keep track of the number of conflicts
	numHostConflicts := 0
	numNameConflicts := 0

	for i := 1; i <= 100; i++ {
		conflict, err := probe(ctx, conn, *candidate)
		if err != nil {
			e = err
			return
		}

		if conflict.hasNone() {
			s = *candidate
			return
		}

		candidate = candidate.Copy()

		if conflict.hostname && (prevConflict.hostname || probeOnce) {
			numHostConflicts++
			candidate.Host = incrementHostname(candidate.Host, numHostConflicts+1)
			conflict.hostname = false
		}

		if conflict.serviceName && (prevConflict.serviceName || probeOnce) {
			numNameConflicts++
			candidate.Name = incrementServiceName(candidate.Name, numNameConflicts+1)
			conflict.serviceName = false
		}

		prevConflict = conflict

		if conflict.hasAny() {
			// If the host finds that its own data is lexicographically earlier,
			// then it defers to the winning host by waiting one second,
			// and then begins probing for this record again. (RFC6762 8.2)
			log.Debug.Println("Increase wait time after receiving conflicting data")
			delay = 1 * time.Second
		}

		log.Debug.Println("Probing wait", delay)
		time.Sleep(delay)
	}

	return
}

func probe(ctx context.Context, conn MDNSConn, service Service) (conflict probeConflict, err error) {
	var queries []*Query
	for _, iface := range service.Interfaces() {
		queries = append(queries, probeQuery(service, iface))
	}

	readCtx, readCancel := context.WithCancel(ctx)
	defer readCancel()

	// Multicast DNS responses received *before* the first probe packet is sent
	// MUST be silently ignored. (RFC6762 8.1)
	conn.Drain(readCtx)
	ch := conn.Read(readCtx)

	queryTime := time.After(1 * time.Millisecond)
	queriesCount := 1

	for {
		select {
		case rsp := <-ch:

			if rsp.iface == nil {
				continue
			}

			reqAs, reqAAAAs, reqSRVs := splitRecords(filterRecords(rsp, &service))

			as := A(service, rsp.iface)
			aaaas := AAAA(service, rsp.iface)

			if len(reqAs) > 0 && len(as) > 0 && areDenyingAs(reqAs, as) {
				log.Debug.Printf("%v:%d@%s denies A\n", rsp.from.IP, rsp.from.Port, rsp.IfaceName())
				log.Debug.Println(reqAs)
				log.Debug.Println(as)
				conflict.hostname = true
			}

			if len(reqAAAAs) > 0 && len(aaaas) > 0 && areDenyingAAAAs(reqAAAAs, aaaas) {
				log.Debug.Printf("%v:%d@%s denies AAAA\n", rsp.from.IP, rsp.from.Port, rsp.IfaceName())
				log.Debug.Println(reqAAAAs)
				log.Debug.Println(aaaas)
				conflict.hostname = true
			}

			// If the service instance name is already taken from another host,
			// we have a service instance name conflict
			conflict.serviceName = len(reqSRVs) > 0

		case <-ctx.Done():
			err = ctx.Err()
			return

		case <-queryTime:
			// Stop on conflict
			if conflict.hasAny() {
				return conflict, err
			}

			// Stop after 3 probe queries
			if queriesCount > 3 {
				return
			}

			queriesCount++
			for _, q := range queries {
				log.Debug.Println("Sending probe", q.iface.Name, q.msg)
				if err := conn.SendQuery(q); err != nil {
					log.Debug.Println("Sending probe err:", err)
				}
			}

			delay := 250 * time.Millisecond
			log.Debug.Println("Waiting for conflicting data", delay)
			queryTime = time.After(delay)
		}
	}
}

func probeQuery(service Service, iface *net.Interface) *Query {
	msg := new(dns.Msg)

	instanceQ := dns.Question{
		Name:   service.EscapedServiceInstanceName(),
		Qtype:  dns.TypeANY,
		Qclass: dns.ClassINET,
	}

	hostQ := dns.Question{
		Name:   service.Hostname(),
		Qtype:  dns.TypeANY,
		Qclass: dns.ClassINET,
	}

	setQuestionUnicast(&instanceQ)
	setQuestionUnicast(&hostQ)

	msg.Question = []dns.Question{instanceQ, hostQ}

	srv := SRV(service)
	as := A(service, iface)
	aaaas := AAAA(service, iface)

	var authority = []dns.RR{srv}
	for _, a := range as {
		authority = append(authority, a)
	}
	for _, aaaa := range aaaas {
		authority = append(authority, aaaa)
	}
	msg.Ns = authority

	return &Query{msg: msg, iface: iface}
}

type probeConflict struct {
	hostname    bool
	serviceName bool
}

func (pr probeConflict) hasNone() bool {
	return !pr.hostname && !pr.serviceName
}

func (pr probeConflict) hasAny() bool {
	return pr.hostname || pr.serviceName
}

func isDenyingA(this *dns.A, that *dns.A) bool {
	if strings.EqualFold(this.Hdr.Name, that.Hdr.Name) {
		log.Debug.Println("Same hosts")

		if !isValidRR(this) {
			log.Debug.Println("Invalid record produces conflict")
			return true
		}

		switch compareIP(this.A.To4(), that.A.To4()) {
		case -1:
			log.Debug.Println("Lexicographical earlier")
		case 1:
			log.Debug.Println("Lexicographical later")
			return true
		default:
			log.Debug.Println("No conflict")
		}
	}

	return false
}

// isDenyingAAAA returns true if this denies that.
func isDenyingAAAA(this *dns.AAAA, that *dns.AAAA) bool {
	if strings.EqualFold(this.Hdr.Name, that.Hdr.Name) {
		log.Debug.Println("Same hosts")
		if !isValidRR(this) {
			log.Debug.Println("Invalid record produces conflict")
			return true
		}

		switch compareIP(this.AAAA.To16(), that.AAAA.To16()) {
		case -1:
			log.Debug.Println("Lexicographical earlier")
		case 1:
			log.Debug.Println("Lexicographical later")
			return true
		default:
			log.Debug.Println("No conflict")
		}
	}

	return false
}

// areDenyingAs returns true if this and that are denying each other.
func areDenyingAs(this []*dns.A, that []*dns.A) bool {
	if len(this) != len(that) {
		log.Debug.Printf("A: different number of records is a conflict (%d != %d)\n", len(this), len(that))
		return true
	}

	sort.Sort(byAIP(this))
	sort.Sort(byAIP(that))

	for i, ti := range this {
		ta := that[i]
		if isDenyingA(ti, ta) {
			return true
		}
	}

	log.Debug.Println("A: same records are no conflict")
	return false
}

func areDenyingAAAAs(this []*dns.AAAA, that []*dns.AAAA) bool {
	if len(this) != len(that) {
		log.Debug.Printf("AAAA: different number of records is a conflict (%d != %d)\n", len(this), len(that))
		return true
	}

	sort.Sort(byAAAAIP(this))
	sort.Sort(byAAAAIP(that))

	for i, ti := range this {
		ta := that[i]
		if isDenyingAAAA(ti, ta) {
			return true
		}
	}

	log.Debug.Println("AAAA: same records are no conflict")
	return false
}

type byAIP []*dns.A

func (a byAIP) Len() int      { return len(a) }
func (a byAIP) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byAIP) Less(i, j int) bool {
	return strings.Compare(a[i].A.To4().String(), a[j].A.To4().String()) == -1
}

type byAAAAIP []*dns.AAAA

func (a byAAAAIP) Len() int      { return len(a) }
func (a byAAAAIP) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byAAAAIP) Less(i, j int) bool {
	return strings.Compare(a[i].AAAA.To16().String(), a[j].AAAA.To16().String()) == -1
}

// isDenyingSRV returns true if this denies that.
func isDenyingSRV(this *dns.SRV, that *dns.SRV) bool {
	if strings.EqualFold(this.Hdr.Name, that.Hdr.Name) {
		log.Debug.Println("Same SRV")
		if !isValidRR(this) {
			log.Debug.Println("Invalid record produces conflict")
			return true
		}

		switch compareSRV(this, that) {
		case -1:
			log.Debug.Println("Lexicographical earlier")
		case 1:
			log.Debug.Println("Lexicographical later")
			return true
		default:
			log.Debug.Println("No conflict")
		}
	}

	return false
}

func isValidRR(rr dns.RR) bool {
	switch r := rr.(type) {
	case *dns.A:
		return !net.IPv4zero.Equal(r.A)
	case *dns.AAAA:
		return !net.IPv6zero.Equal(r.AAAA)
	case *dns.SRV:
		return len(r.Target) > 0 && r.Port != 0
	default:
	}

	return true
}

func compareIP(this net.IP, that net.IP) int {
	count := len(this)
	if count > len(that) {
		count = len(that)
	}

	for i := 0; i < count; i++ {
		if this[i] < that[i] {
			return -1
		} else if this[i] > that[i] {
			return 1
		}
	}

	if len(this) < len(that) {
		return -1
	} else if len(this) > len(that) {
		return 1
	}
	return 0
}

func compareSRV(this *dns.SRV, that *dns.SRV) int {
	if this.Priority < that.Priority {
		return -1
	} else if this.Priority > that.Priority {
		return 1
	}

	if this.Weight < that.Weight {
		return -1
	} else if this.Weight > that.Weight {
		return 1
	}

	if this.Port < that.Port {
		return -1
	} else if this.Port > that.Port {
		return 1
	}

	return strings.Compare(this.Target, that.Target)
}
