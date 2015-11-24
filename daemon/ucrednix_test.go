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

type ucrednixSuite struct {
	ucred *sys.Ucred
	err   error
}

var _ = check.Suite(&ucrednixSuite{})

func (s *ucrednixSuite) getUcred(fd, level, opt int) (*sys.Ucred, error) {
	return s.ucred, s.err
}

func (s *ucrednixSuite) SetUpSuite(c *check.C) {
	getUcred = s.getUcred
}

func (s *ucrednixSuite) TearDownTest(c *check.C) {
	s.ucred = nil
	s.err = nil
}
func (s *ucrednixSuite) TearDownSuite(c *check.C) {
	getUcred = sys.GetsockoptUcred
}

func (s *ucrednixSuite) TestAcceptConnRemoteAddrString(c *check.C) {
	s.ucred = &sys.Ucred{Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	wl := &ucrednixListener{l.(*net.UnixListener)}

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)

	c.Check(conn.RemoteAddr().String(), check.Matches, "42:.*")
}

func (s *ucrednixSuite) TestAcceptErrors(c *check.C) {
	s.ucred = &sys.Ucred{Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	c.Assert(l.Close(), check.IsNil)

	wl := &ucrednixListener{l.(*net.UnixListener)}

	_, err = wl.Accept()
	c.Assert(err, check.NotNil)
}

func (s *ucrednixSuite) TestUcredErrors(c *check.C) {
	s.err = errors.New("oopsie")
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	wl := &ucrednixListener{l.(*net.UnixListener)}

	_, err = wl.Accept()
	c.Assert(err, check.Equals, s.err)
}
