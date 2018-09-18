// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	defer l.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	wl := &ucrednetListener{l}

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	c.Check(remoteAddr, check.Matches, "pid=100;uid=42;.*")
	pid, uid, _, err := ucrednetGet(remoteAddr)
	c.Check(pid, check.Equals, uint32(100))
	c.Check(uid, check.Equals, uint32(42))
	c.Check(err, check.IsNil)
}

func (s *ucrednetSuite) TestNonUnix(c *check.C) {
	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, check.IsNil)
	defer l.Close()

	addr := l.Addr().String()

	go func() {
		cli, err := net.Dial("tcp", addr)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	wl := &ucrednetListener{l}

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	c.Check(remoteAddr, check.Matches, "pid=;uid=;.*")
	pid, uid, _, err := ucrednetGet(remoteAddr)
	c.Check(pid, check.Equals, ucrednetNoProcess)
	c.Check(uid, check.Equals, ucrednetNobody)
	c.Check(err, check.Equals, errNoID)
}

func (s *ucrednetSuite) TestAcceptErrors(c *check.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	c.Assert(l.Close(), check.IsNil)

	wl := &ucrednetListener{l}

	_, err = wl.Accept()
	c.Assert(err, check.NotNil)
}

func (s *ucrednetSuite) TestUcredErrors(c *check.C) {
	s.err = errors.New("oopsie")
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	defer l.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	wl := &ucrednetListener{l}

	_, err = wl.Accept()
	c.Assert(err, check.Equals, s.err)
}

func (s *ucrednetSuite) TestGetNoUid(c *check.C) {
	pid, uid, _, err := ucrednetGet("pid=100;uid=;")
	c.Check(err, check.Equals, errNoID)
	c.Check(pid, check.Equals, uint32(100))
	c.Check(uid, check.Equals, ucrednetNobody)
}

func (s *ucrednetSuite) TestGetBadUid(c *check.C) {
	pid, uid, _, err := ucrednetGet("pid=100;uid=hello;")
	c.Check(err, check.NotNil)
	c.Check(pid, check.Equals, uint32(100))
	c.Check(uid, check.Equals, ucrednetNobody)
}

func (s *ucrednetSuite) TestGetNonUcrednet(c *check.C) {
	pid, uid, _, err := ucrednetGet("hello")
	c.Check(err, check.Equals, errNoID)
	c.Check(pid, check.Equals, ucrednetNoProcess)
	c.Check(uid, check.Equals, ucrednetNobody)
}

func (s *ucrednetSuite) TestGetNothing(c *check.C) {
	pid, uid, _, err := ucrednetGet("")
	c.Check(err, check.Equals, errNoID)
	c.Check(pid, check.Equals, ucrednetNoProcess)
	c.Check(uid, check.Equals, ucrednetNobody)
}

func (s *ucrednetSuite) TestGet(c *check.C) {
	pid, uid, socket, err := ucrednetGet("pid=100;uid=42;socket=/run/snap.socket")
	c.Check(err, check.IsNil)
	c.Check(pid, check.Equals, uint32(100))
	c.Check(uid, check.Equals, uint32(42))
	c.Check(socket, check.Equals, "/run/snap.socket")
}
