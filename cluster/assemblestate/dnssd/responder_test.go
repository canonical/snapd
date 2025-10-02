package dnssd

import (
	"context"
	"github.com/brutella/dnssd/log"
	"github.com/miekg/dns"
	"net"
	"testing"
	"time"
)

func TestRemove(t *testing.T) {
	cfg := Config{
		Name: "Test",
		Type: "_asdf._tcp",
		Port: 1234,
	}
	si, err := NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}

	msg := new(dns.Msg)
	msg.Answer = []dns.RR{SRV(si), TXT(si)}

	answers := []dns.RR{SRV(si), TXT(si), PTR(si)}
	unknown := remove(msg.Answer, answers)

	if x := len(unknown); x != 1 {
		t.Fatal(x)
	}

	rr := unknown[0]
	if _, ok := rr.(*dns.PTR); !ok {
		t.Fatal("Invalid type", rr)
	}
}

func TestRegisterServiceWithExplicitIP(t *testing.T) {
	cfg := Config{
		Host:   "Computer",
		Name:   "Test",
		Type:   "_asdf._tcp",
		Domain: "local",
		Port:   12345,
		Ifaces: []string{"lo0"},
	}
	sv, err := NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sv.ifaceIPs = map[string][]net.IP{
		"lo0": {net.IP{192, 168, 0, 123}},
	}

	conn := newTestConn()
	otherConn := newTestConn()
	conn.in = otherConn.out
	conn.out = otherConn.in

	ctx, cancel := context.WithCancel(context.Background())
	t.Run("resolver", func(t *testing.T) {
		t.Parallel()

		lookupCtx, lookupCancel := context.WithTimeout(ctx, 5*time.Second)

		defer lookupCancel()
		defer cancel()

		srv, err := lookupInstance(lookupCtx, "Test._asdf._tcp.local.", otherConn)
		if err != nil {
			t.Fatal(err)
		}

		if is, want := srv.Name, "Test"; is != want {
			t.Fatalf("%v != %v", is, want)
		}

		if is, want := srv.Type, "_asdf._tcp"; is != want {
			t.Fatalf("%v != %v", is, want)
		}

		if is, want := srv.Host, "Computer"; is != want {
			t.Fatalf("%v != %v", is, want)
		}

		ips := srv.IPsAtInterface(&net.Interface{Name: "lo0"})
		if is, want := len(ips), 1; is != want {
			t.Fatalf("%v != %v", is, want)
		}

		if is, want := ips[0].String(), "192.168.0.123"; is != want {
			t.Fatalf("%v != %v", is, want)
		}
	})

	t.Run("responder", func(t *testing.T) {
		t.Parallel()

		r := newResponder(conn)
		r.addManaged(sv) // don't probe
		r.Respond(ctx)
	})
}

type expectedIP struct {
	advType  IPType
	expected []net.IP
}

func TestRegisterServiceWithSpecifiedAdvertisedIP(t *testing.T) {
	log.Debug.Enable()

	v4 := net.IP{192, 168, 0, 123}
	v6 := net.ParseIP("fe80::1")

	var expectedIPs = map[string]expectedIP{
		"v4 only":            {IPv4, []net.IP{v4}},
		"v6 only":            {IPv6, []net.IP{v6}},
		"both / unspecified": {IPType(0), []net.IP{v4, v6}},
	}

	for name, expected := range expectedIPs {
		t.Run(name, func(t *testing.T) {
			cfg := Config{
				Host:            "Computer",
				Name:            "Test",
				Type:            "_asdf._tcp",
				Domain:          "local",
				Port:            12345,
				Ifaces:          []string{"lo0"},
				AdvertiseIPType: expected.advType,
			}
			sv, err := NewService(cfg)
			if err != nil {
				t.Fatal(err)
			}
			sv.ifaceIPs = map[string][]net.IP{
				"lo0": {v4, v6},
			}

			conn := newTestConn()
			otherConn := newTestConn()
			conn.in = otherConn.out
			conn.out = otherConn.in

			ctx, cancel := context.WithCancel(context.Background())
			t.Run("resolver", func(t *testing.T) {
				t.Parallel()

				lookupCtx, lookupCancel := context.WithTimeout(ctx, 5*time.Second)

				defer lookupCancel()
				defer cancel()

				srv, err := lookupInstance(lookupCtx, "Test._asdf._tcp.local.", otherConn)
				if err != nil {
					t.Fatal(err)
				}

				if is, want := srv.Name, "Test"; is != want {
					t.Fatalf("%v != %v", is, want)
				}

				if is, want := srv.Type, "_asdf._tcp"; is != want {
					t.Fatalf("%v != %v", is, want)
				}

				if is, want := srv.Host, "Computer"; is != want {
					t.Fatalf("%v != %v", is, want)
				}

				ips := srv.IPsAtInterface(&net.Interface{Name: "lo0"})
				if is, want := len(ips), len(expected.expected); is != want {
					t.Fatalf("%v != %v", is, want)
				}

				for i, ip := range ips { // this should always be the same order as a records are processed before aaaa records
					if is, want := ip, expected.expected[i]; !is.Equal(want) {
						t.Fatalf("%v != %v", is, want)
					}
				}
			})

			t.Run("responder", func(t *testing.T) {
				t.Parallel()

				r := newResponder(conn)
				r.addManaged(sv) // don't probe
				r.Respond(ctx)
			})
		})
	}
}
