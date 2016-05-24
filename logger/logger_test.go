// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package logger

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&LogSuite{})

type LogSuite struct {
	sysbuf *bytes.Buffer
}

func (s *LogSuite) SetUpTest(c *C) {
	c.Assert(logger, Equals, NullLogger)

	// we do not want to pollute syslog in our tests (and sbuild
	// will also not let us do that)
	newSyslog = func() (*log.Logger, error) {
		s.sysbuf = bytes.NewBuffer(nil)
		return log.New(s.sysbuf, "", SyslogFlags), nil
	}
}

func (s *LogSuite) TearDownTest(c *C) {
	SetLogger(NullLogger)
	newSyslog = newSyslogImpl
}

func (s *LogSuite) TestDefault(c *C) {
	if logger != nil {
		SetLogger(nil)
	}
	c.Check(logger, IsNil)

	err := SimpleSetup()
	c.Check(err, IsNil)
	c.Check(logger, NotNil)
	SetLogger(nil)
}

func (s *LogSuite) TestNew(c *C) {
	var buf bytes.Buffer
	l, err := NewConsoleLog(&buf, DefaultFlags)
	c.Assert(err, IsNil)
	c.Assert(l, NotNil)
	c.Check(l.sys, NotNil)
	c.Check(l.log, NotNil)
}

func (s *LogSuite) TestDebugf(c *C) {
	var logbuf bytes.Buffer
	l, err := NewConsoleLog(&logbuf, DefaultFlags)
	c.Assert(err, IsNil)

	SetLogger(l)

	Debugf("xyzzy")
	c.Check(s.sysbuf.String(), Matches, `(?m).*logger_test\.go:\d+: DEBUG: xyzzy`)
	c.Check(logbuf.String(), Equals, "")
}

func (s *LogSuite) TestDebugfEnv(c *C) {
	var logbuf bytes.Buffer
	l, err := NewConsoleLog(&logbuf, DefaultFlags)
	c.Assert(err, IsNil)

	SetLogger(l)

	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")

	Debugf("xyzzy")
	c.Check(s.sysbuf.String(), Matches, `(?m).*logger_test\.go:\d+: DEBUG: xyzzy`)
	c.Check(logbuf.String(), testutil.Contains, `DEBUG: xyzzy`)
}

func (s *LogSuite) TestNoticef(c *C) {
	var logbuf bytes.Buffer
	l, err := NewConsoleLog(&logbuf, DefaultFlags)
	c.Assert(err, IsNil)

	SetLogger(l)

	Noticef("xyzzy")
	c.Check(s.sysbuf.String(), Matches, `(?m).*logger_test\.go:\d+: xyzzy`)
	c.Check(logbuf.String(), Matches, `(?m).*logger_test\.go:\d+: xyzzy`)
}

func (s *LogSuite) TestPanicf(c *C) {
	var logbuf bytes.Buffer
	l, err := NewConsoleLog(&logbuf, DefaultFlags)
	c.Assert(err, IsNil)

	SetLogger(l)

	c.Check(func() { Panicf("xyzzy") }, Panics, "xyzzy")
	c.Check(s.sysbuf.String(), Matches, `(?m).*logger_test\.go:\d+: PANIC xyzzy`)
	c.Check(logbuf.String(), Matches, `(?m).*logger_test\.go:\d+: PANIC xyzzy`)
}

func (s *LogSuite) TestSyslogFails(c *C) {
	var logbuf bytes.Buffer

	// pretend syslog is not available (e.g. because of no /dev/log in
	// a chroot or something)
	newSyslog = func() (*log.Logger, error) {
		return nil, fmt.Errorf("nih nih")
	}

	// ensure a warning is displayed
	l, err := NewConsoleLog(&logbuf, DefaultFlags)
	c.Assert(err, IsNil)
	c.Check(logbuf.String(), Matches, `(?m).*:\d+: WARNING: can not create syslog logger`)

	// ensure that even without a syslog the console log works and we
	// do not crash
	logbuf.Reset()
	SetLogger(l)
	Noticef("I do not want to crash")
	c.Check(logbuf.String(), Matches, `(?m).*logger_test\.go:\d+: I do not want to crash`)

}
