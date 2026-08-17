// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	proxy "github.com/snapcore/snapd/cmd/snapd/snap-userdb-proxy"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type proxySuite struct {
	testutil.BaseTest
}

var _ = Suite(&proxySuite{})

func (s *proxySuite) SetUpTest(c *C) {
	_, restore := logger.MockLogger()
	s.AddCleanup(restore)
}

func (s *proxySuite) TestPeerIsConfinedSnapLabels(c *C) {
	tests := []struct {
		label string
		ok    bool
	}{
		{label: "snap.hello.hello", ok: true},
		{label: "snap.hello.hello (complain)", ok: true},
		{label: "unconfined", ok: false},
		{label: "", ok: false},
		{label: "foo.snap.bar", ok: false},
	}

	for _, t := range tests {
		c.Logf("label %q", t.label)
		restore := proxy.MockGetsockoptPeerSec(func(*net.UnixConn) (string, error) {
			return t.label, nil
		})

		a, b := unixPair(c)
		label, ok, err := proxy.PeerIsSnap(a)
		c.Check(err, IsNil)
		c.Check(ok, Equals, t.ok)
		c.Check(label, Equals, t.label)

		a.Close()
		b.Close()
		restore()
	}
}

func (s *proxySuite) TestProxyRejectsNonSnapPeer(c *C) {
	cliConn, cancel := s.setupProxyAndClientWithPeerLabel(c, "unused", "unconfined")
	defer cancel()
	defer cliConn.Close()

	req := []byte(`{"method":"io.systemd.UserDatabase.GetUserRecord","parameters":{"service":"io.snapcraft.UserDBProxy","userName":"test-user"}}` + "\x00")
	_, err := cliConn.Write(req)
	c.Assert(err, IsNil)

	// timeout if there are no responses
	err = cliConn.SetReadDeadline(time.Now().Add(time.Second))
	c.Assert(err, IsNil)

	buf := make([]byte, 1)
	n, err := cliConn.Read(buf)
	c.Check(n, Equals, 0)
	c.Assert(err, NotNil)
}

func (s *proxySuite) TestProxyRejectsOversizedMessage(c *C) {
	restore := proxy.MockMaxMessageSize(10)
	defer restore()

	cliConn, cancel := s.setupProxyAndClient(c, "unused")
	defer cancel()
	defer cliConn.Close()

	content := make([]byte, 15)
	_, err := rand.Read(content)
	c.Assert(err, IsNil)

	// timeout if there are no responses
	err = cliConn.SetReadDeadline(time.Now().Add(time.Second))
	c.Assert(err, IsNil)

	_, err = cliConn.Write(content)
	c.Assert(err, IsNil)

	buf := make([]byte, 1)
	_, err = cliConn.Read(buf)
	c.Assert(err, Equals, io.EOF)
}

func (s *proxySuite) TestProxyForwardsSingleReply(c *C) {
	expected := `{"parameters":{"record":{"userName":"test-user"},"incomplete":false}}`
	muxAddr, reqChan, cleanup := mockMultiplexer(c, expected)
	defer cleanup()

	cliConn, cancel := s.setupProxyAndClient(c, muxAddr)
	defer cancel()
	defer cliConn.Close()

	req := []byte(`{"method":"io.systemd.UserDatabase.GetUserRecord","parameters":{"service":"io.snapcraft.UserDBProxy","userName":"test-user"}}` + "\x00")
	_, err := cliConn.Write(req)
	c.Assert(err, IsNil)

	// timeout if there are no responses
	err = cliConn.SetReadDeadline(time.Now().Add(time.Second))
	c.Assert(err, IsNil)

	reply, err := readVarlinkMessage(cliConn)
	c.Assert(err, IsNil)
	c.Check(string(reply), Equals, expected)

	reqMethods := getMuxRequests(c, reqChan, 1)
	c.Check(reqMethods, DeepEquals, []string{"io.systemd.UserDatabase.GetUserRecord"})
}

func (s *proxySuite) TestProxyForwardsStreamingReplies(c *C) {
	expected := []string{
		`{"parameters":{"userName":"user1","groupName":"group1"},"continues":true}`,
		`{"parameters":{"userName":"user2","groupName":"group2"}}`,
	}
	muxAddr, reqChan, cleanup := mockMultiplexer(c, expected...)
	defer cleanup()

	cliConn, cancel := s.setupProxyAndClient(c, muxAddr)
	defer cancel()
	defer cliConn.Close()

	req := []byte(`{"method":"io.systemd.UserDatabase.GetMemberships","parameters":{"service":"io.snapcraft.UserDBProxy"},"more":true}` + "\x00")
	_, err := cliConn.Write(req)
	c.Assert(err, IsNil)

	// timeout if there are no responses
	err = cliConn.SetReadDeadline(time.Now().Add(time.Second))
	c.Assert(err, IsNil)

	// "continues" is propagated
	reply, err := readVarlinkMessage(cliConn)
	c.Assert(err, IsNil)
	c.Check(string(reply), Equals, `{"parameters":{"userName":"user1","groupName":"group1"},"continues":true}`)

	reply, err = readVarlinkMessage(cliConn)
	c.Assert(err, IsNil)
	c.Check(string(reply), Equals, `{"parameters":{"userName":"user2","groupName":"group2"}}`)

	reqMethods := getMuxRequests(c, reqChan, 1)
	c.Check(reqMethods, DeepEquals, []string{"io.systemd.UserDatabase.GetMemberships"})
}

func (s *proxySuite) TestProxyForwardsMultiplexerError(c *C) {
	muxAddr, reqChan, cleanup := mockMultiplexer(c,
		`{"error":"io.systemd.UserDatabase.NoRecordFound"}`)
	defer cleanup()

	cliConn, cancel := s.setupProxyAndClient(c, muxAddr)
	defer cancel()
	defer cliConn.Close()

	req := []byte(`{"method":"io.systemd.UserDatabase.GetUserRecord","parameters":{"service":"io.snapcraft.UserDBProxy","userName":"missing"}}` + "\x00")
	_, err := cliConn.Write(req)
	c.Assert(err, IsNil)

	// timeout if there are no responses
	err = cliConn.SetReadDeadline(time.Now().Add(time.Second))
	c.Assert(err, IsNil)

	reply, err := readVarlinkMessage(cliConn)
	c.Assert(err, IsNil)
	c.Check(string(reply), Equals, `{"parameters":null,"error":"io.systemd.UserDatabase.NoRecordFound"}`)

	reqMethods := getMuxRequests(c, reqChan, 1)
	c.Check(reqMethods, DeepEquals, []string{"io.systemd.UserDatabase.GetUserRecord"})
}

func (s *proxySuite) TestProxyRejectsMissingService(c *C) {
	cliConn, cancel := s.setupProxyAndClient(c, "unix:/nonexistent/multiplexer.sock")
	defer cancel()
	defer cliConn.Close()

	req := []byte(`{"method":"io.systemd.UserDatabase.GetUserRecord","parameters":{"userName":"root"}}` + "\x00")
	_, err := cliConn.Write(req)
	c.Assert(err, IsNil)

	err = cliConn.SetReadDeadline(time.Now().Add(time.Second))
	c.Assert(err, IsNil)

	reply, err := readVarlinkMessage(cliConn)
	c.Assert(err, IsNil)
	c.Check(varlinkError(c, reply), Equals, "io.systemd.UserDatabase.BadService")
}

func (s *proxySuite) TestProxyRejectsWrongService(c *C) {
	cliConn, cancel := s.setupProxyAndClient(c, "unix:/nonexistent/multiplexer.sock")
	defer cancel()
	defer cliConn.Close()

	req := []byte(`{"method":"io.systemd.UserDatabase.GetUserRecord","parameters":{"service":"io.systemd.Multiplexer","userName":"root"}}` + "\x00")
	_, err := cliConn.Write(req)
	c.Assert(err, IsNil)

	// timeout if there are no responses
	err = cliConn.SetReadDeadline(time.Now().Add(time.Second))
	c.Assert(err, IsNil)

	reply, err := readVarlinkMessage(cliConn)
	c.Assert(err, IsNil)
	c.Check(varlinkError(c, reply), Equals, "io.systemd.UserDatabase.BadService")

}

func (s *proxySuite) TestProxyUnknownMethod(c *C) {
	muxAddr, _, cleanup := mockMultiplexer(c, `{"error":"io.systemd.UserDatabase.NoRecordFound"}`)
	defer cleanup()

	cliConn, cancel := s.setupProxyAndClient(c, muxAddr)
	defer cancel()
	defer cliConn.Close()

	req := []byte(`{"method":"io.systemd.UserDatabase.MadeUpMethod","parameters":{"service":"io.snapcraft.UserDBProxy"}}` + "\x00")
	_, err := cliConn.Write(req)
	c.Assert(err, IsNil)

	// timeout if there are no responses
	err = cliConn.SetReadDeadline(time.Now().Add(time.Second))
	c.Assert(err, IsNil)

	raw, err := readVarlinkMessage(cliConn)
	c.Assert(err, IsNil)

	var reply struct {
		Error string `json:"error,omitempty"`
	}
	c.Assert(json.Unmarshal(raw, &reply), IsNil)
	c.Check(reply.Error, Equals, "org.varlink.service.MethodNotFound")
}

// startProxyClient creates a proxy to the supplied address and returns a
// connection to that proxy.
func (s *proxySuite) setupProxyAndClient(c *C, addr string) (net.Conn, context.CancelFunc) {
	return s.setupProxyAndClientWithPeerLabel(c, addr, "snap.hello.hello")
}

func (s *proxySuite) setupProxyAndClientWithPeerLabel(c *C, addr, label string) (net.Conn, context.CancelFunc) {
	restore := proxy.MockGetsockoptPeerSec(func(*net.UnixConn) (string, error) {
		return label, nil
	})

	sockPath := filepath.Join(c.MkDir(), "proxy.sock")
	l, err := net.Listen("unix", sockPath)
	c.Assert(err, IsNil)
	s.AddCleanup(func() { l.Close() })

	p, err := proxy.NewProxy(addr)
	c.Assert(err, IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- p.Serve(ctx, l)
	}()

	conn, err := net.Dial("unix", sockPath)
	c.Assert(err, IsNil)
	return conn, func() {
		cancel()
		select {
		case err := <-serveDone:
			c.Check(err, IsNil)
		case <-time.After(time.Second):
			c.Fatal("proxy.Serve did not stop")
		}
		restore()
	}
}

// unixPair returns a pair of connected AF_UNIX sockets for tests that
// need a real net.UnixConn without spinning up a listener.
func unixPair(c *C) (net.Conn, net.Conn) {
	fds, err := unixSocketpair()
	c.Assert(err, IsNil)
	a := netConnFromFd(c, fds[0], "a")
	b := netConnFromFd(c, fds[1], "b")
	return a, b
}

// readVarlinkMessage reads from conn until a NUL terminator is seen and
// returns the payload (without the NUL).
func readVarlinkMessage(conn net.Conn) ([]byte, error) {
	var out []byte
	buf := make([]byte, 1)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}
		if buf[0] == 0 {
			return out, nil
		}
		out = append(out, buf[0])
	}
}

func varlinkError(c *C, raw []byte) string {
	var reply struct {
		Error string `json:"error"`
	}
	c.Assert(json.Unmarshal(raw, &reply), IsNil)
	return reply.Error
}

// mockMultiplexer creates a service that accepts connetions and replies with
// the given records. The requests are sent through the returned channel.
func mockMultiplexer(c *C, replies ...string) (addr string, requests <-chan []byte, cleanup func()) {
	sockPath := filepath.Join(c.MkDir(), "multiplexer.sock")
	l, err := net.Listen("unix", sockPath)
	c.Assert(err, IsNil)

	reqCh := make(chan []byte, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer l.Close()
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, err := reader.ReadBytes(0)
		if err != nil {
			return
		}
		reqCh <- req[:len(req)-1]

		for _, reply := range replies {
			_, _ = conn.Write(append([]byte(reply), 0))
		}
	}()

	cleanup = func() {
		_ = l.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			c.Fatal("mock Multiplexer did not stop")
		}
	}
	return "unix:" + sockPath, reqCh, cleanup
}

// getMuxRequests takes a reqChan (returned by the mocked multiplexer) and a
// number of expected requests sent to it. Returns the method called in each
// request.
func getMuxRequests(c *C, reqChan <-chan []byte, numReqs int) []string {
	type reqStruct struct {
		Method     string         `json:"method"`
		Parameters map[string]any `json:"parameters"`
	}

	var reqMethods []string
	for i := 0; i < numReqs; i++ {
		select {
		case raw := <-reqChan:
			var req reqStruct
			c.Assert(json.Unmarshal(raw, &req), IsNil)
			c.Check(req.Parameters["service"], Equals, "io.systemd.Multiplexer")
			reqMethods = append(reqMethods, req.Method)

		case <-time.After(time.Second):
			c.Fatalf("multiplexer received %d requests but expected %d", i, numReqs)
		}
	}

	return reqMethods
}
