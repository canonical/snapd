// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build structuredlogging

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

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/testutil"
)

func TestStructured(t *testing.T) { TestingT(t) }

var _ = Suite(&LogStructuredSuite{})

type LogStructuredSuite struct {
	testutil.BaseTest
	logbuf        *bytes.Buffer
	restoreLogger func()
}

func (s *LogStructuredSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	os.Setenv("SNAPD_JSON_LOGGING", "1")
	defer os.Unsetenv("SNAPD_JSON_LOGGING")
	s.logbuf, s.restoreLogger = logger.MockLogger()
}

func (s *LogStructuredSuite) TearDownTest(c *C) {
	s.restoreLogger()
}

var _ = Suite(&LogStructuredSuite{})

type TestSourceLogEntry struct {
	File     string
	Line     int32
	Function string
}

type TestLogEntry struct {
	Msg    string
	Level  string
	Attr   string
	Time   string
	Source TestSourceLogEntry
}

func decodeStructuredEntries(c *C, buf *bytes.Buffer) []TestLogEntry {
	raw := bytes.TrimSpace(buf.Bytes())
	if len(raw) == 0 {
		return nil
	}

	lines := bytes.Split(raw, []byte("\n"))
	entries := make([]TestLogEntry, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var data TestLogEntry
		err := json.Unmarshal(line, &data)
		c.Assert(err, IsNil)
		entries = append(entries, data)
	}

	return entries
}

func (s *LogStructuredSuite) TestNewStructured(c *C) {
	os.Setenv("SNAPD_JSON_LOGGING", "1")
	defer os.Unsetenv("SNAPD_JSON_LOGGING")
	var buf bytes.Buffer
	l := logger.New(&buf, logger.DefaultFlags, nil)
	c.Assert(l, NotNil)
}

func (s *LogStructuredSuite) TestDebugfStructured(c *C) {
	logger.Debug("xyzzy")
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *LogStructuredSuite) TestTrace(c *C) {
	logger.Trace("xyzzy")
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *LogStructuredSuite) TestTraceEnvStructured(c *C) {
	os.Setenv("SNAPD_TRACE", "1")
	defer os.Unsetenv("SNAPD_TRACE")

	logger.Trace("xyzzy", "attr", "val")
	data := TestLogEntry{}
	err := json.Unmarshal(s.logbuf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Msg, Equals, "xyzzy")
	c.Check(data.Level, Equals, "TRACE")
	c.Check(data.Attr, Equals, "val")
	c.Check(data.Source.File, Equals, "structured_logger_test.go")
}

func (s *LogStructuredSuite) TestTraceEnvStructuredDeduplicatesRepeatedEntry(c *C) {
	os.Setenv("SNAPD_TRACE", "1")
	defer os.Unsetenv("SNAPD_TRACE")

	logger.Trace("xyzzy", "attr", "val")
	logger.Trace("xyzzy", "attr", "val")

	entries := decodeStructuredEntries(c, s.logbuf)
	c.Assert(entries, HasLen, 1)
	c.Check(entries[0].Msg, Equals, "xyzzy")
	c.Check(entries[0].Level, Equals, "TRACE")
	c.Check(entries[0].Attr, Equals, "val")
}

func (s *LogStructuredSuite) TestTraceEnvStructuredAllowsDistinctEntries(c *C) {
	os.Setenv("SNAPD_TRACE", "1")
	defer os.Unsetenv("SNAPD_TRACE")

	logger.Trace("xyzzy", "attr", "val")
	logger.Trace("xyzzy", "attr", "different")

	entries := decodeStructuredEntries(c, s.logbuf)
	c.Assert(entries, HasLen, 2)
	c.Check(entries[0].Msg, Equals, "xyzzy")
	c.Check(entries[0].Level, Equals, "TRACE")
	c.Check(entries[0].Attr, Equals, "val")
	c.Check(entries[1].Msg, Equals, "xyzzy")
	c.Check(entries[1].Level, Equals, "TRACE")
	c.Check(entries[1].Attr, Equals, "different")
}

func (s *LogStructuredSuite) TestTraceEnvDebugStructured(c *C) {
	os.Setenv("SNAPD_TRACE", "1")
	defer os.Unsetenv("SNAPD_TRACE")

	logger.Debug("xyzzy")
	data := TestLogEntry{}
	err := json.Unmarshal(s.logbuf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Msg, Equals, "xyzzy")
	c.Check(data.Level, Equals, "DEBUG")
	c.Check(data.Attr, Equals, "")
	c.Check(data.Source.File, Equals, "structured_logger_test.go")
}

func (s *LogStructuredSuite) TestNoticeStructured(c *C) {
	logger.Notice("xyzzy")
	data := TestLogEntry{}
	err := json.Unmarshal(s.logbuf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Msg, Equals, "xyzzy")
	c.Check(data.Level, Equals, "NOTICE")
	c.Check(data.Attr, Equals, "")
	c.Check(data.Source.File, Equals, "structured_logger_test.go")
}

func (s *LogStructuredSuite) TestNoTimestamp(c *C) {
	os.Setenv("SNAPD_JSON_LOGGING", "1")
	defer os.Unsetenv("SNAPD_JSON_LOGGING")

	var buf bytes.Buffer
	l := logger.New(&buf, log.Lshortfile, nil)
	l.Notice("xyzzy")

	var data map[string]any
	err := json.Unmarshal(buf.Bytes(), &data)
	c.Check(err, IsNil)
	_, ok := data["time"]
	c.Check(ok, Equals, false)
	_, ok = data["source"]
	c.Check(ok, Equals, true)
	_, ok = data["msg"]
	c.Check(ok, Equals, true)
	_, ok = data["level"]
	c.Check(ok, Equals, true)
}

func (s *LogStructuredSuite) TestPanicfStructured(c *C) {
	c.Check(func() { logger.Panicf("xyzzy") }, Panics, "xyzzy")
	data := TestLogEntry{}
	err := json.Unmarshal(s.logbuf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Msg, Equals, "PANIC xyzzy")
	c.Check(data.Level, Equals, "NOTICE")
	c.Check(data.Attr, Equals, "")
}

func (s *LogStructuredSuite) TestWithLoggerLockStructured(c *C) {

	logger.Noticef("xyzzy")
	called := false
	logger.WithLoggerLock(func() {
		called = true
		data := TestLogEntry{}
		err := json.Unmarshal(s.logbuf.Bytes(), &data)
		c.Check(err, IsNil)
		c.Check(data.Msg, Equals, "xyzzy")
		c.Check(data.Level, Equals, "NOTICE")
		c.Check(data.Attr, Equals, "")
		c.Check(data.Source.File, Equals, "structured_logger_test.go")
	})
	c.Check(called, Equals, true)
}

func (s *LogStructuredSuite) TestNoGuardDebugStructured(c *C) {
	debugValue, ok := os.LookupEnv("SNAPD_DEBUG")
	if ok {
		defer func() {
			os.Setenv("SNAPD_DEBUG", debugValue)
		}()
		os.Unsetenv("SNAPD_DEBUG")
	}

	logger.NoGuardDebugf("xyzzy")
	data := TestLogEntry{}
	err := json.Unmarshal(s.logbuf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Msg, Equals, "xyzzy")
	c.Check(data.Level, Equals, "DEBUG")
	c.Check(data.Attr, Equals, "")
	c.Check(data.Source.File, Equals, "structured_logger_test.go")
}

func (s *LogStructuredSuite) TestIntegrationDebugFromKernelCmdlineStructured(c *C) {
	os.Setenv("SNAPD_JSON_LOGGING", "1")
	defer os.Unsetenv("SNAPD_JSON_LOGGING")
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
	l := logger.New(&buf, logger.DefaultFlags, nil)
	l.Debug("xyzzy")
	data := TestLogEntry{}
	err = json.Unmarshal(buf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Msg, Equals, "xyzzy")
	c.Check(data.Level, Equals, "DEBUG")
	c.Check(data.Attr, Equals, "")
}

func (s *LogStructuredSuite) TestIntegrationTraceFromKernelCmdlineStructured(c *C) {
	// must enable actually checking the command line, because by default the
	// logger package will skip checking for the kernel command line parameter
	// if it detects it is in a test because otherwise we would have to mock the
	// cmdline in many many many more tests that end up using a logger
	restore := logger.ProcCmdlineMustMock(false)
	defer restore()

	mockProcCmdline := filepath.Join(c.MkDir(), "proc-cmdline")
	err := os.WriteFile(mockProcCmdline, []byte("console=tty panic=-1 snapd.debug=1 tag.features=1\n"), 0644)
	c.Assert(err, IsNil)
	restore = kcmdline.MockProcCmdline(mockProcCmdline)
	defer restore()

	var buf bytes.Buffer
	l := logger.New(&buf, logger.DefaultFlags, nil)
	l.Trace("xyzzy")
	data := TestLogEntry{}
	err = json.Unmarshal(buf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Msg, Equals, "xyzzy")
	c.Check(data.Level, Equals, "TRACE")
	c.Check(data.Attr, Equals, "")
}

func (s *LogSuite) TestBootSetupStructuredLogger(c *C) {
	// env shenanigans
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restoreProcCmdline := logger.ProcCmdlineMustMock(false)
	defer restoreProcCmdline()

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
	err := os.WriteFile(cmdlineFile, []byte("mocked panic=-1 tag.features=1"), 0644)
	c.Assert(err, IsNil)
	restore := kcmdline.MockProcCmdline(cmdlineFile)
	defer restore()
	os.Setenv("TERM", "dumb")
	err = logger.BootSetup()
	c.Assert(err, IsNil)
	l := logger.GetLogger()
	c.Check(l, NotNil)
	_, ok := l.(*logger.StructuredLog)
	c.Assert(ok, Equals, true)
}

func (s *LogStructuredSuite) TestStartupTimestampMsgStructured(c *C) {
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
	data := TestLogEntry{}
	err := json.Unmarshal(s.logbuf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Level, Equals, "DEBUG")
	c.Check(data.Msg, Equals, `-- snap startup {"stage":"foo to bar", "time":"1652697792.022312"}`)

	var m msgTimestamp
	start := strings.LastIndex(data.Msg, "{")
	c.Assert(start, Not(Equals), -1)
	stamp := data.Msg[start:]
	err = json.Unmarshal([]byte(stamp), &m)
	c.Assert(err, IsNil)
	c.Check(m, Equals, msgTimestamp{
		Stage: "foo to bar",
		Time:  "1652697792.022312",
	})
}

func (s *LogStructuredSuite) TestForceDebugStructured(c *C) {
	os.Setenv("SNAPD_JSON_LOGGING", "1")
	defer os.Unsetenv("SNAPD_JSON_LOGGING")

	var buf bytes.Buffer
	l := logger.New(&buf, logger.DefaultFlags, &logger.LoggerOptions{ForceDebug: true})
	l.Debug("xyzzy")
	data := TestLogEntry{}
	err := json.Unmarshal(buf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Level, Equals, "DEBUG")
	c.Check(data.Msg, Equals, "xyzzy")
}

func (s *LogStructuredSuite) TestMockDebugLoggerStructured(c *C) {
	os.Setenv("SNAPD_JSON_LOGGING", "1")
	defer os.Unsetenv("SNAPD_JSON_LOGGING")

	logbuf, restore := logger.MockDebugLogger()
	defer restore()
	logger.Debugf("xyzzy")
	data := TestLogEntry{}
	err := json.Unmarshal(logbuf.Bytes(), &data)
	c.Check(err, IsNil)
	c.Check(data.Level, Equals, "DEBUG")
	c.Check(data.Msg, Equals, "xyzzy")
}
