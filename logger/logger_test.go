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
	"log"
	"testing"

	. "launchpad.net/gocheck"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&LogSuite{})

type LogSuite struct{}

func (s *LogSuite) SetUpTest(c *C) {
	c.Assert(logger, Equals, NullLogger)
}

func (s *LogSuite) TearDownTest(c *C) {
	SetLogger(NullLogger)
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
	var sysbuf bytes.Buffer
	l, err := NewConsoleLog(&logbuf, DefaultFlags)
	c.Assert(err, IsNil)

	l.sys = log.New(&sysbuf, "", SyslogFlags)
	SetLogger(l)

	Debugf("xyzzy")
	c.Check(sysbuf.String(), Matches, `(?m).*logger_test\.go:\d+: DEBUG: xyzzy`)
	c.Check(logbuf.String(), Equals, "")
}

func (s *LogSuite) TestNoticef(c *C) {
	var logbuf bytes.Buffer
	var sysbuf bytes.Buffer
	l, err := NewConsoleLog(&logbuf, DefaultFlags)
	c.Assert(err, IsNil)

	l.sys = log.New(&sysbuf, "", SyslogFlags)
	SetLogger(l)

	Noticef("xyzzy")
	c.Check(sysbuf.String(), Matches, `(?m).*logger_test\.go:\d+: xyzzy`)
	c.Check(logbuf.String(), Matches, `(?m).*logger_test\.go:\d+: xyzzy`)
}

func (s *LogSuite) TestPanicf(c *C) {
	var logbuf bytes.Buffer
	var sysbuf bytes.Buffer
	l, err := NewConsoleLog(&logbuf, DefaultFlags)
	c.Assert(err, IsNil)

	l.sys = log.New(&sysbuf, "", SyslogFlags)
	SetLogger(l)

	c.Check(func() { Panicf("xyzzy") }, Panics, "xyzzy")
	c.Check(sysbuf.String(), Matches, `(?m).*logger_test\.go:\d+: PANIC xyzzy`)
	c.Check(logbuf.String(), Matches, `(?m).*logger_test\.go:\d+: PANIC xyzzy`)
}
