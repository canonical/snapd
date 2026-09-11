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
	"context"
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

func (s *ucrednetSuite) TestAcceptConnContext(c *check.C) {
	readlinkCalls := 0
	restore := MockOsReadlink(func(path string) (string, error) {
		readlinkCalls++
		c.Check(path, check.Equals, "/proc/100/exe")
		return "/usr/bin/snap", nil
	})
	defer restore()

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
	c.Check(readlinkCalls, check.Equals, 1)

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.PIDForPolkit, check.Equals, int32(100))
	name, err := u.UntrustedProcessExeName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "/usr/bin/snap")
	c.Check(readlinkCalls, check.Equals, 1)
	c.Check(conn.RemoteAddr().String(), check.Equals, conn.(*ucrednetConn).Conn.RemoteAddr().String())
}

func (s *ucrednetSuite) TestAcceptConnContextUnreadableExe(c *check.C) {
	lookupErr := errors.New("cannot read executable")
	restore := MockOsReadlink(func(path string) (string, error) {
		c.Check(path, check.Equals, "/proc/100/exe")
		return "", lookupErr
	})
	defer restore()

	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	sock := filepath.Join(c.MkDir(), "sock")
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

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.PIDForPolkit, check.Equals, int32(100))
	name, err := u.UntrustedProcessExeName()
	c.Check(err, check.Equals, lookupErr)
	c.Check(name, check.Equals, "")
}

func (s *ucrednetSuite) TestAcceptConnContextInstanceName(c *check.C) {
	lookupCalls := 0
	restore := MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		lookupCalls++
		c.Check(pid, check.Equals, 100)
		return "some-snap_instance", nil
	})
	defer restore()

	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	sock := filepath.Join(c.MkDir(), "sock")
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
	c.Check(lookupCalls, check.Equals, 1)

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap_instance")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, sock)
	c.Check(u.PIDForPolkit, check.Equals, int32(100))
	c.Check(lookupCalls, check.Equals, 1)
}

func (s *ucrednetSuite) TestAcceptConnContextUnknownInstanceName(c *check.C) {
	lookupErr := errors.New("cannot find snap security tag")
	lookupCalls := 0
	restore := MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		lookupCalls++
		c.Check(pid, check.Equals, 100)
		return "", lookupErr
	})
	defer restore()

	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	sock := filepath.Join(c.MkDir(), "sock")
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

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.Equals, lookupErr)
	c.Check(name, check.Equals, "")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, sock)
	c.Check(u.PIDForPolkit, check.Equals, int32(100))
	c.Check(lookupCalls, check.Equals, 1)
}

func (s *ucrednetSuite) TestEmptyNames(c *check.C) {
	u := NewUcrednet("", "", 42, "/run/snap.socket")
	name, err := u.InstanceName()
	c.Check(name, check.Equals, "")
	c.Check(err, check.ErrorMatches, "snap instance name is not available")
	name, err = u.UntrustedProcessExeName()
	c.Check(name, check.Equals, "")
	c.Check(err, check.ErrorMatches, "process executable name is not available")
}

func (s *ucrednetSuite) TestString(c *check.C) {
	var u *ucrednet
	c.Check(u.String(), check.Equals, "snap=;uid=;socket=;")
	u = NewUcrednet("some-snap_instance", "", 42, "/run/snap.socket")
	u.PIDForPolkit = 100
	c.Check(u.String(), check.Equals, "snap=some-snap_instance;uid=42;socket=/run/snap.socket;")
	u = NewUcrednet("", "", 42, "/run/snap.socket")
	c.Check(u.String(), check.Equals, "snap=;uid=42;socket=/run/snap.socket;")
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

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
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

func (s *ucrednetSuite) TestGetNothing(c *check.C) {
	u, err := ucrednetGet(context.Background())
	c.Check(err, check.Equals, errNoID)
	c.Check(u, check.IsNil)
}

func (s *ucrednetSuite) TestGet(c *check.C) {
	original := NewUcrednet("some-snap", "/usr/bin/snap", 42, "/run/snap.socket")
	ctx := ucrednetWithCredentials(context.Background(), original)
	original.Uid = 0

	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	name, err = u.UntrustedProcessExeName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "/usr/bin/snap")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
}

func (s *ucrednetSuite) TestGetWithInterface(c *check.C) {
	ctx := ucrednetWithCredentials(context.Background(), NewUcrednet("some-snap", "", 42, "/run/snap.socket"))
	ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
	u, ifaces, err := ucrednetGetWithInterfaces(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.DeepEquals, []string{"snap-refresh-observe"})

	// interfaces are optional
	ctx = ucrednetWithCredentials(context.Background(), u)
	u, ifaces, err = ucrednetGetWithInterfaces(ctx)
	c.Assert(err, check.IsNil)
	name, err = u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.IsNil)
}

func (s *ucrednetSuite) TestAttachInterface(c *check.C) {
	ctx := ucrednetWithCredentials(context.Background(), NewUcrednet("some-snap", "", 42, "/run/snap.socket"))
	ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
	u, ifaces, err := ucrednetGetWithInterfaces(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.DeepEquals, []string{"snap-refresh-observe"})
}

func (s *ucrednetSuite) TestAttachInterfaceRepeatedly(c *check.C) {
	ctx := ucrednetWithCredentials(context.Background(), NewUcrednet("some-snap", "", 42, "/run/snap.socket"))
	for i := 0; i < 2; i++ {
		ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
		u, ifaces, err := ucrednetGetWithInterfaces(ctx)
		c.Assert(err, check.IsNil)
		name, err := u.InstanceName()
		c.Check(err, check.IsNil)
		c.Check(name, check.Equals, "some-snap")
		c.Check(u.Uid, check.Equals, uint32(42))
		c.Check(u.Socket, check.Equals, "/run/snap.socket")
		c.Check(ifaces, check.DeepEquals, []string{"snap-refresh-observe"})
	}
}

func (s *ucrednetSuite) TestAttachInterfaceMultiple(c *check.C) {
	ctx := ucrednetWithCredentials(context.Background(), NewUcrednet("some-snap", "", 42, "/run/snap.socket"))
	ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
	ctx = ucrednetAttachInterface(ctx, "snap-interfaces-requests-control")
	ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
	ctx = ucrednetAttachInterface(ctx, "foo")

	u, ifaces, err := ucrednetGetWithInterfaces(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.DeepEquals, []string{
		"snap-refresh-observe",
		"snap-interfaces-requests-control",
		"foo",
	})
}
