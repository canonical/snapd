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
	"os"
	"path"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	. "github.com/snapcore/snapd/systemd"
)

type journalTestSuite struct {
	journalDir          string
	journalNamespaceDir string
}

var _ = Suite(&journalTestSuite{})

func (j *journalTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	j.journalDir = path.Join(dirs.SnapSystemdRunDir, "journal")
	c.Assert(os.MkdirAll(j.journalDir, 0755), IsNil)

	j.journalNamespaceDir = path.Join(dirs.SnapSystemdRunDir, "journal.test")
	c.Assert(os.MkdirAll(j.journalNamespaceDir, 0755), IsNil)
}

func (j *journalTestSuite) TestStreamFileErrorNoIdentifier(c *C) {
	jout, err := NewJournalStreamFile(JournalStreamFileParams{
		Priority: syslog.LOG_INFO,
	})
	c.Assert(err, ErrorMatches, "internal error: cannot setup a journal stream without an identifier")
	c.Assert(jout, IsNil)
}

func (j *journalTestSuite) TestStreamFileErrorNoPath(c *C) {
	jout, err := NewJournalStreamFile(JournalStreamFileParams{
		Identifier: "foobar",
		Priority:   syslog.LOG_INFO,
	})
	c.Assert(err, ErrorMatches, ".*no such file or directory")
	c.Assert(jout, IsNil)
}

func (j *journalTestSuite) testStreamFileHeader(c *C, journalDir, namespace string) {
	fakePath := path.Join(journalDir, "stdout")
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

		expectedHdrLen := len("foobar") + 1 + len("foobar.service") + 1 + 2 + 2 + 2 + 2 + 2
		hdrBuf := make([]byte, expectedHdrLen)
		hdrLen, err := conn.Read(hdrBuf)
		c.Assert(err, IsNil)
		c.Assert(hdrLen, Equals, expectedHdrLen)
		c.Check(hdrBuf, DeepEquals, []byte("foobar\nfoobar.service\n6\n0\n0\n0\n0\n"))

		data := make([]byte, 4096)
		sz, err := conn.Read(data)
		c.Assert(err, IsNil)
		c.Assert(sz > 0, Equals, true)
		c.Check(data[0:sz], DeepEquals, []byte("hello from unit tests"))

		doneCh <- struct{}{}
	}()

	jout, err := NewJournalStreamFile(JournalStreamFileParams{
		Namespace:  namespace,
		Identifier: "foobar",
		UnitName:   "foobar.service",
		Priority:   syslog.LOG_INFO,
	})
	c.Assert(err, IsNil)
	c.Assert(jout, NotNil)

	_, err = jout.WriteString("hello from unit tests")
	c.Assert(err, IsNil)
	defer jout.Close()

	<-doneCh
}

func (j *journalTestSuite) TestStreamFileHeader(c *C) {
	j.testStreamFileHeader(c, j.journalDir, "")
}

func (j *journalTestSuite) TestNamespaceStream(c *C) {
	j.testStreamFileHeader(c, j.journalNamespaceDir, "test")
}
