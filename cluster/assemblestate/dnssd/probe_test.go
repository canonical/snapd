package dnssd

import (
	"github.com/miekg/dns"

	"context"
	"net"
	"testing"
	"time"
)

var testAddr = net.UDPAddr{
	IP:   net.IP{},
	Port: 1234,
	Zone: "",
}

var testIface = &net.Interface{
	Index:        0,
	MTU:          0,
	Name:         "lo0",
	HardwareAddr: []byte{},
	Flags:        net.FlagUp,
}

type testConn struct {
	read chan *Request

	in  chan *dns.Msg
	out chan *dns.Msg
}

func newTestConn() *testConn {
	c := &testConn{
		read: make(chan *Request),
		in:   make(chan *dns.Msg),
		out:  make(chan *dns.Msg),
	}

	return c
}

func (c *testConn) SendQuery(q *Query) error {
	go func() {
		c.out <- q.msg
	}()
	return nil
}

func (c *testConn) SendResponse(resp *Response) error {
	go func() {
		c.out <- resp.msg
	}()

	return nil
}

func (c *testConn) Read(ctx context.Context) <-chan *Request {
	go c.start(ctx)
	return c.read
}

func (c *testConn) Drain(ctx context.Context) {}

func (c *testConn) Close() {}

func (c *testConn) start(ctx context.Context) {
	for {
		select {
		case msg := <-c.in:
			req := &Request{msg: msg, from: &testAddr, iface: testIface}
			c.read <- req
		case <-ctx.Done():
			return
		}
	}
}

// TestProbing tests probing by using 2 services with the same
// service instance name and host name.Once the first services
// is announced, the probing for the second service should give
func TestProbing(t *testing.T) {
	// log.Debug.Enable()
	testIface, _ = net.InterfaceByName("lo0")
	if testIface == nil {
		testIface, _ = net.InterfaceByName("lo")
	}
	if testIface == nil {
		t.Fatal("can not find the local interface")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	conn := newTestConn()
	otherConn := newTestConn()
	conn.in = otherConn.out
	conn.out = otherConn.in

	cfg := Config{
		Name:   "My Service",
		Type:   "_hap._tcp",
		Host:   "My Computer",
		Port:   12334,
		Ifaces: []string{testIface.Name},
	}
	srv, err := NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv.ifaceIPs = map[string][]net.IP{
		testIface.Name: []net.IP{net.IP{192, 168, 0, 122}},
	}

	r := newResponder(otherConn)
	go func() {
		rcfg := cfg.Copy()
		rsrv, err := NewService(rcfg)
		if err != nil {
			t.Fatal(err)
		}
		rsrv.ifaceIPs = map[string][]net.IP{
			testIface.Name: []net.IP{net.IP{192, 168, 0, 123}},
		}

		rctx, rcancel := context.WithCancel(ctx)
		defer rcancel()

		r.addManaged(rsrv)
		r.Respond(rctx)
	}()

	resolved, err := probeService(ctx, conn, srv, 500*time.Millisecond, true)

	if x := err; x != nil {
		t.Fatal(x)
	}

	if is, want := resolved.Host, "My-Computer-2"; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}

	if is, want := resolved.Name, "My Service (2)"; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}

	cancel()
}

func TestIsLexicographicLater(t *testing.T) {
	this := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "MyPrinter.local.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    TTLHostname,
		},
		A: net.ParseIP("169.254.99.200"),
	}

	that := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "MyPrinter.local.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    TTLHostname,
		},
		A: net.ParseIP("169.254.200.50"),
	}

	if is, want := compareIP(this.A.To4(), that.A.To4()), -1; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}

	if is, want := compareIP(that.A.To4(), this.A.To4()), 1; is != want {
		t.Fatalf("is=%v want=%v", is, want)
	}
}

func TestDenyingAs(t *testing.T) {
	tests := []struct {
		This   []*dns.A
		That   []*dns.A
		Result bool
	}{
		{
			This: []*dns.A{
				&dns.A{
					Hdr: dns.RR_Header{
						Name:   "MyPrinter.local.",
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    TTLHostname,
					},
					A: net.ParseIP("169.254.99.200"),
				},
			},
			That: []*dns.A{
				&dns.A{
					Hdr: dns.RR_Header{
						Name:   "MyPrinter.local.",
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    TTLHostname,
					},
					A: net.ParseIP("169.254.99.200"),
				},
			},
			Result: false,
		},
		{
			This: []*dns.A{
				&dns.A{
					Hdr: dns.RR_Header{
						Name:   "MyPrinter.local.",
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    TTLHostname,
					},
					A: net.ParseIP("169.254.99.200"),
				},
			},
			That:   []*dns.A{},
			Result: true,
		},
		{
			This: []*dns.A{},
			That: []*dns.A{
				&dns.A{
					Hdr: dns.RR_Header{
						Name:   "MyPrinter.local.",
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    TTLHostname,
					},
					A: net.ParseIP("169.254.99.200"),
				},
			},
			Result: true,
		},
		{
			This:   []*dns.A{},
			That:   []*dns.A{},
			Result: false,
		},
	}

	for _, test := range tests {
		if is, want := areDenyingAs(test.This, test.That), test.Result; is != want {
			t.Fatalf("%v != %v is=%v want=%v", test.This, test.That, is, want)
		}
	}
}
