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

package seclog_test

import (
	"bytes"
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/testutil"
)

type JournalSuite struct {
	testutil.BaseTest
	buf *bytes.Buffer
}

var _ = Suite(&JournalSuite{})

func (s *JournalSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.buf = &bytes.Buffer{}
}

func (s *JournalSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *JournalSuite) TestNewJournalWriterDefaultLevel(c *C) {
	jw := seclog.NewJournalWriter(s.buf)
	c.Assert(jw, NotNil)

	// default level is LevelInfo, syslog.LOG_INFO == 6
	_, err := jw.Write([]byte("hello"))
	c.Assert(err, IsNil)
	c.Check(s.buf.String(), Equals, "<6>hello")
}

func (s *JournalSuite) TestSetLevelAndWrite(c *C) {
	jw := seclog.NewJournalWriter(s.buf)

	tests := []struct {
		level          seclog.Level
		expectedPrefix string
	}{
		{seclog.LevelDebug, "<7>"},    // LOG_DEBUG
		{seclog.LevelInfo, "<6>"},     // LOG_INFO
		{seclog.LevelWarn, "<4>"},     // LOG_WARNING
		{seclog.LevelError, "<3>"},    // LOG_ERR
		{seclog.LevelCritical, "<2>"}, // LOG_CRIT
	}

	for _, t := range tests {
		s.buf.Reset()
		jw.SetLevel(t.level)
		msg := []byte("test message")
		n, err := jw.Write(msg)
		c.Assert(err, IsNil)
		c.Check(n, Equals, len(msg),
			Commentf("level %v", t.level))
		c.Check(s.buf.String(), Equals, t.expectedPrefix+"test message",
			Commentf("level %v", t.level))
	}
}

func (s *JournalSuite) TestWriteByteCountExcludesPrefix(c *C) {
	jw := seclog.NewJournalWriter(s.buf)

	msg := []byte("payload")
	n, err := jw.Write(msg)
	c.Assert(err, IsNil)
	// n must equal len(msg), not len("<6>payload")
	c.Check(n, Equals, len(msg))
}

type errWriter struct {
	err error
}

func (w *errWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

func (s *JournalSuite) TestWritePropagatesError(c *C) {
	expected := fmt.Errorf("disk full")
	jw := seclog.NewJournalWriter(&errWriter{err: expected})

	n, err := jw.Write([]byte("data"))
	c.Check(err, Equals, expected)
	c.Check(n, Equals, 0)
}

// closeRecorder implements io.WriteCloser and records whether Close was called.
type closeRecorder struct {
	bytes.Buffer
	closed bool
}

func (cr *closeRecorder) Close() error {
	cr.closed = true
	return nil
}

func (s *JournalSuite) TestCloseForwardsToUnderlyingWriter(c *C) {
	cr := &closeRecorder{}
	jw := seclog.NewJournalWriter(cr)

	err := jw.Close()
	c.Assert(err, IsNil)
	c.Check(cr.closed, Equals, true)
}

func (s *JournalSuite) TestCloseWithNonCloserReturnsNil(c *C) {
	// bytes.Buffer does not implement io.Closer
	jw := seclog.NewJournalWriter(s.buf)

	err := jw.Close()
	c.Assert(err, IsNil)
}
