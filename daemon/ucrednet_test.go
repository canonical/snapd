// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2024 Canonical Ltd
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

package daemon

import (
	"errors"
	"net"
	"path/filepath"
	sys "syscall"

	"gopkg.in/check.v1"
)

type ucrednetSuite struct {
	ucred *sys.Ucred
	err   error
}

var _ = check.Suite(&ucrednetSuite{})

func (s *ucrednetSuite) getUcred(fd, level, opt int) (*sys.Ucred, error) {
	return s.ucred, s.err
}

func (s *ucrednetSuite) SetUpSuite(c *check.C) {
	getUcred = s.getUcred
}

func (s *ucrednetSuite) TearDownTest(c *check.C) {
	s.ucred = nil
	s.err = nil
}
func (s *ucrednetSuite) TearDownSuite(c *check.C) {
	getUcred = sys.GetsockoptUcred
}

func (s *ucrednetSuite) TestAcceptConnRemoteAddrString(c *check.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	wl := &ucrednetListener{Listener: l}

	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	c.Check(remoteAddr, check.Matches, "pid=100;uid=42;.*")
	u, err := ucrednetGet(remoteAddr)
	c.Assert(err, check.IsNil)
	c.Check(u.Pid, check.Equals, int32(100))
	c.Check(u.Uid, check.Equals, uint32(42))
}

func (s *ucrednetSuite) TestNonUnix(c *check.C) {
	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, check.IsNil)

	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	addr := l.Addr().String()

	go func() {
		cli, err := net.Dial("tcp", addr)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	c.Check(remoteAddr, check.Matches, "pid=;uid=;.*")
	u, err := ucrednetGet(remoteAddr)
	c.Check(u, check.IsNil)
	c.Check(err, check.Equals, errNoID)
}

func (s *ucrednetSuite) TestAcceptErrors(c *check.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	c.Assert(l.Close(), check.IsNil)

	wl := &ucrednetListener{Listener: l}

	_, err = wl.Accept()
	c.Assert(err, check.NotNil)
}

func (s *ucrednetSuite) TestUcredErrors(c *check.C) {
	s.err = errors.New("oopsie")
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)

	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	_, err = wl.Accept()
	c.Assert(err, check.Equals, s.err)
}

func (s *ucrednetSuite) TestIdempotentClose(c *check.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	wl := &ucrednetListener{Listener: l}

	c.Assert(wl.Close(), check.IsNil)
	c.Assert(wl.Close(), check.IsNil)
}

func (s *ucrednetSuite) TestGetNoUid(c *check.C) {
	u, err := ucrednetGet("pid=100;uid=;socket=;")
	c.Check(err, check.Equals, errNoID)
	c.Check(u, check.IsNil)
}

func (s *ucrednetSuite) TestGetBadUid(c *check.C) {
	u, err := ucrednetGet("pid=100;uid=4294967296;socket=;")
	c.Check(err, check.Equals, errNoID)
	c.Check(u, check.IsNil)
}

func (s *ucrednetSuite) TestGetNonUcrednet(c *check.C) {
	u, err := ucrednetGet("hello")
	c.Check(err, check.Equals, errNoID)
	c.Check(u, check.IsNil)
}

func (s *ucrednetSuite) TestGetNothing(c *check.C) {
	u, err := ucrednetGet("")
	c.Check(err, check.Equals, errNoID)
	c.Check(u, check.IsNil)
}

func (s *ucrednetSuite) TestGet(c *check.C) {
	u, err := ucrednetGet("pid=100;uid=42;socket=/run/snap.socket;")
	c.Assert(err, check.IsNil)
	c.Check(u.Pid, check.Equals, int32(100))
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")

	u, err = ucrednetGet("pid=100;uid=42;socket=/run/snap.socket;iface=snap-refresh-observe;")
	c.Assert(err, check.IsNil)
	c.Check(u.Pid, check.Equals, int32(100))
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
}

func (s *ucrednetSuite) TestGetSneak(c *check.C) {
	u, err := ucrednetGet("pid=100;uid=42;socket=/run/snap.socket;pid=0;uid=0;socket=/tmp/my.socket")
	c.Check(err, check.Equals, errNoID)
	c.Check(u, check.IsNil)
}

func (s *ucrednetSuite) TestGetWithInterface(c *check.C) {
	u, ifaces, err := ucrednetGetWithInterfaces("pid=100;uid=42;socket=/run/snap.socket;iface=snap-refresh-observe;")
	c.Assert(err, check.IsNil)
	c.Check(u.Pid, check.Equals, int32(100))
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.DeepEquals, []string{"snap-refresh-observe"})

	// iface is optional
	u, ifaces, err = ucrednetGetWithInterfaces("pid=100;uid=42;socket=/run/snap.socket;")
	c.Assert(err, check.IsNil)
	c.Check(u.Pid, check.Equals, int32(100))
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.IsNil)
}

func (s *ucrednetSuite) TestAttachInterface(c *check.C) {
	remoteAddr := ucrednetAttachInterface("pid=100;uid=42;socket=/run/snap.socket;", "snap-refresh-observe")
	c.Check(remoteAddr, check.Equals, "pid=100;uid=42;socket=/run/snap.socket;iface=snap-refresh-observe;")

	u, ifaces, err := ucrednetGetWithInterfaces(remoteAddr)
	c.Assert(err, check.IsNil)
	c.Check(u.Pid, check.Equals, int32(100))
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.DeepEquals, []string{"snap-refresh-observe"})
}

func (s *ucrednetSuite) TestAttachInterfaceRepeatedly(c *check.C) {
	remoteAddr := "pid=100;uid=42;socket=/run/snap.socket;"
	for i := 0; i < 2; i++ {
		remoteAddr = ucrednetAttachInterface(remoteAddr, "snap-refresh-observe")
		c.Check(remoteAddr, check.Equals, "pid=100;uid=42;socket=/run/snap.socket;iface=snap-refresh-observe;")

		u, ifaces, err := ucrednetGetWithInterfaces(remoteAddr)
		c.Assert(err, check.IsNil)
		c.Check(u.Pid, check.Equals, int32(100))
		c.Check(u.Uid, check.Equals, uint32(42))
		c.Check(u.Socket, check.Equals, "/run/snap.socket")
		c.Check(ifaces, check.DeepEquals, []string{"snap-refresh-observe"})
	}
}

func (s *ucrednetSuite) TestAttachInterfaceMultiple(c *check.C) {
	remoteAddr := ucrednetAttachInterface("pid=100;uid=42;socket=/run/snap.socket;", "snap-refresh-observe")
	c.Check(remoteAddr, check.Equals, "pid=100;uid=42;socket=/run/snap.socket;iface=snap-refresh-observe;")

	remoteAddr = ucrednetAttachInterface(remoteAddr, "snap-interfaces-requests-control")
	c.Check(remoteAddr, check.Equals, "pid=100;uid=42;socket=/run/snap.socket;iface=snap-refresh-observe&snap-interfaces-requests-control;")

	remoteAddr = ucrednetAttachInterface(remoteAddr, "snap-refresh-observe")
	c.Check(remoteAddr, check.Equals, "pid=100;uid=42;socket=/run/snap.socket;iface=snap-refresh-observe&snap-interfaces-requests-control;")

	remoteAddr = ucrednetAttachInterface(remoteAddr, "foo")
	c.Check(remoteAddr, check.Equals, "pid=100;uid=42;socket=/run/snap.socket;iface=snap-refresh-observe&snap-interfaces-requests-control&foo;")

	u, ifaces, err := ucrednetGetWithInterfaces(remoteAddr)
	c.Assert(err, check.IsNil)
	c.Check(u.Pid, check.Equals, int32(100))
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.DeepEquals, []string{
		"snap-refresh-observe",
		"snap-interfaces-requests-control",
		"foo",
	})
}
