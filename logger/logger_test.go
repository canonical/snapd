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

package logger_test

import (
	"bytes"
	"log"
	"os"
	"runtime"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&LogSuite{})

type LogSuite struct {
	logbuf        *bytes.Buffer
	restoreLogger func()
}

func (s *LogSuite) SetUpTest(c *C) {
	s.logbuf, s.restoreLogger = logger.MockLogger()
}

func (s *LogSuite) TearDownTest(c *C) {
	s.restoreLogger()
}

func (s *LogSuite) TestDefault(c *C) {
	// env shenanigans
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	oldTerm, hadTerm := os.LookupEnv("TERM")
	defer func() {
		if hadTerm {
			os.Setenv("TERM", oldTerm)
		} else {
			os.Unsetenv("TERM")
		}
	}()

	if logger.GetLogger() != nil {
		logger.SetLogger(nil)
	}
	c.Check(logger.GetLogger(), IsNil)

	os.Setenv("TERM", "dumb")
	err := logger.SimpleSetup()
	c.Assert(err, IsNil)
	c.Check(logger.GetLogger(), NotNil)
	c.Check(logger.GetLoggerFlags(), Equals, logger.DefaultFlags)

	os.Unsetenv("TERM")
	err = logger.SimpleSetup()
	c.Assert(err, IsNil)
	c.Check(logger.GetLogger(), NotNil)
	c.Check(logger.GetLoggerFlags(), Equals, log.Lshortfile)
}

func (s *LogSuite) TestNew(c *C) {
	var buf bytes.Buffer
	l, err := logger.New(&buf, logger.DefaultFlags)
	c.Assert(err, IsNil)
	c.Assert(l, NotNil)
}

func (s *LogSuite) TestDebugf(c *C) {
	logger.Debugf("xyzzy")
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *LogSuite) TestDebugfEnv(c *C) {
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")

	logger.Debugf("xyzzy")
	c.Check(s.logbuf.String(), testutil.Contains, `DEBUG: xyzzy`)
}

func (s *LogSuite) TestNoticef(c *C) {
	logger.Noticef("xyzzy")
	c.Check(s.logbuf.String(), Matches, `(?m).*logger_test\.go:\d+: xyzzy`)
}

func (s *LogSuite) TestPanicf(c *C) {
	c.Check(func() { logger.Panicf("xyzzy") }, Panics, "xyzzy")
	c.Check(s.logbuf.String(), Matches, `(?m).*logger_test\.go:\d+: PANIC xyzzy`)
}
