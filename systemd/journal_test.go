// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package systemd_test

import (
	"log/syslog"
	"net"
	"path"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/snapd/systemd"
)

type journalTestSuite struct{}

var _ = Suite(&journalTestSuite{})

func (j *journalTestSuite) TestStreamFileErrorNoPath(c *C) {
	restore := MockJournalStdoutPath(path.Join(c.MkDir(), "fake-journal"))
	defer restore()

	jout, err := NewJournalStreamFile("foobar", syslog.LOG_INFO, false)
	c.Assert(err, ErrorMatches, ".*no such file or directory")
	c.Assert(jout, IsNil)
}

func (j *journalTestSuite) TestStreamFileHeader(c *C) {
	fakePath := path.Join(c.MkDir(), "fake-journal")
	restore := MockJournalStdoutPath(fakePath)
	defer restore()

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: fakePath})
	c.Assert(err, IsNil)
	defer listener.Close()

	doneCh := make(chan struct{}, 1)

	go func() {
		defer func() { close(doneCh) }()

		// see https://github.com/systemd/systemd/blob/97a33b126c845327a3a19d6e66f05684823868fb/src/journal/journal-send.c#L424
		conn, err := listener.AcceptUnix()
		c.Assert(err, IsNil)
		defer conn.Close()

		expectedHdrLen := len("foobar") + 1 + 1 + 2 + 2 + 2 + 2 + 2
		hdrBuf := make([]byte, expectedHdrLen)
		hdrLen, err := conn.Read(hdrBuf)
		c.Assert(err, IsNil)
		c.Assert(hdrLen, Equals, expectedHdrLen)
		c.Check(hdrBuf, DeepEquals, []byte("foobar\n\n6\n0\n0\n0\n0\n"))

		data := make([]byte, 4096)
		sz, err := conn.Read(data)
		c.Assert(err, IsNil)
		c.Assert(sz > 0, Equals, true)
		c.Check(data[0:sz], DeepEquals, []byte("hello from unit tests"))

		doneCh <- struct{}{}
	}()

	jout, err := NewJournalStreamFile("foobar", syslog.LOG_INFO, false)
	c.Assert(err, IsNil)
	c.Assert(jout, NotNil)

	_, err = jout.WriteString("hello from unit tests")
	c.Assert(err, IsNil)
	defer jout.Close()

	<-doneCh
}
