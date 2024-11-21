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
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&LogSuite{})

type LogSuite struct {
	testutil.BaseTest
	logbuf        logger.MockedLogger
	restoreLogger func()
}

func (s *LogSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
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
	err := logger.SimpleSetup(nil)
	c.Assert(err, IsNil)
	c.Check(logger.GetLogger(), NotNil)
	c.Check(logger.GetLoggerFlags(), Equals, logger.DefaultFlags)

	os.Unsetenv("TERM")
	err = logger.SimpleSetup(nil)
	c.Assert(err, IsNil)
	c.Check(logger.GetLogger(), NotNil)
	c.Check(logger.GetLoggerFlags(), Equals, log.Lshortfile)
}

func (s *LogSuite) TestBootSetup(c *C) {
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

	cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
	err := os.WriteFile(cmdlineFile, []byte("mocked panic=-1"), 0644)
	c.Assert(err, IsNil)
	restore := kcmdline.MockProcCmdline(cmdlineFile)
	defer restore()
	os.Setenv("TERM", "dumb")
	err = logger.BootSetup()
	c.Assert(err, IsNil)
	c.Check(logger.GetLogger(), NotNil)
	c.Check(logger.GetLoggerFlags(), Equals, logger.DefaultFlags)
	c.Check(logger.GetQuiet(), Equals, false)

	cmdlineFile = filepath.Join(c.MkDir(), "cmdline")
	err = os.WriteFile(cmdlineFile, []byte("mocked panic=-1 quiet"), 0644)
	c.Assert(err, IsNil)
	restore = kcmdline.MockProcCmdline(cmdlineFile)
	defer restore()
	os.Unsetenv("TERM")
	err = logger.BootSetup()
	c.Assert(err, IsNil)
	c.Check(logger.GetLogger(), NotNil)
	c.Check(logger.GetLoggerFlags(), Equals, log.Lshortfile)
	c.Check(logger.GetQuiet(), Equals, true)
}

func (s *LogSuite) TestNew(c *C) {
	var buf bytes.Buffer
	l, err := logger.New(&buf, logger.DefaultFlags, nil)
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

func (s *LogSuite) TestMockLoggerReadWriteThreadsafe(c *C) {
	var t tomb.Tomb
	t.Go(func() error {
		for i := 0; i < 100; i++ {
			logger.Noticef("foo")
			logger.Noticef("bar")
		}
		return nil
	})
	for i := 0; i < 10; i++ {
		logger.Noticef(s.logbuf.String())
	}
	err := t.Wait()
	c.Check(err, IsNil)
}

func (s *LogSuite) TestNoGuardDebug(c *C) {
	debugValue, ok := os.LookupEnv("SNAPD_DEBUG")
	if ok {
		defer func() {
			os.Setenv("SNAPD_DEBUG", debugValue)
		}()
		os.Unsetenv("SNAPD_DEBUG")
	}

	logger.NoGuardDebugf("xyzzy")
	c.Check(s.logbuf.String(), testutil.Contains, `DEBUG: xyzzy`)
}

func (s *LogSuite) TestIntegrationDebugFromKernelCmdline(c *C) {
	// must enable actually checking the command line, because by default the
	// logger package will skip checking for the kernel command line parameter
	// if it detects it is in a test because otherwise we would have to mock the
	// cmdline in many many many more tests that end up using a logger
	restore := logger.ProcCmdlineMustMock(false)
	defer restore()

	mockProcCmdline := filepath.Join(c.MkDir(), "proc-cmdline")
	err := os.WriteFile(mockProcCmdline, []byte("console=tty panic=-1 snapd.debug=1\n"), 0644)
	c.Assert(err, IsNil)
	restore = kcmdline.MockProcCmdline(mockProcCmdline)
	defer restore()

	var buf bytes.Buffer
	l, err := logger.New(&buf, logger.DefaultFlags, nil)
	c.Assert(err, IsNil)
	l.Debug("xyzzy")
	c.Check(buf.String(), testutil.Contains, `DEBUG: xyzzy`)
}

func (s *LogSuite) TestStartupTimestampMsg(c *C) {
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")

	type msgTimestamp struct {
		Stage string `json:"stage"`
		Time  string `json:"time"`
	}

	now := time.Date(2022, time.May, 16, 10, 43, 12, 22312000, time.UTC)
	logger.MockTimeNow(func() time.Time {
		return now
	})
	logger.StartupStageTimestamp("foo to bar")
	msg := strings.TrimSpace(s.logbuf.String())
	c.Assert(msg, Matches, `.* DEBUG: -- snap startup \{"stage":"foo to bar", "time":"1652697792.022312"\}$`)

	var m msgTimestamp
	start := strings.LastIndex(msg, "{")
	c.Assert(start, Not(Equals), -1)
	stamp := msg[start:]
	err := json.Unmarshal([]byte(stamp), &m)
	c.Assert(err, IsNil)
	c.Check(m, Equals, msgTimestamp{
		Stage: "foo to bar",
		Time:  "1652697792.022312",
	})
}

func (s *LogSuite) TestForceDebug(c *C) {
	var buf bytes.Buffer
	l, err := logger.New(&buf, logger.DefaultFlags, &logger.LoggerOptions{ForceDebug: true})
	c.Assert(err, IsNil)
	l.Debug("xyzzy")
	c.Check(buf.String(), testutil.Contains, `DEBUG: xyzzy`)
}
