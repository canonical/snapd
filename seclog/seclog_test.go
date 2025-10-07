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
	"encoding/json"
	"fmt"
	"io"
	"log/syslog"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/testutil"
)

type SecLogSuite struct {
	testutil.BaseTest
	buf   *bytes.Buffer
	appID string
}

var _ = Suite(&SecLogSuite{})

func TestSecLog(t *testing.T) { TestingT(t) }

func (s *SecLogSuite) SetUpSuite(c *C) {
	s.buf = &bytes.Buffer{}
	s.appID = "canonical.snapd"
}

func (s *SecLogSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.buf.Reset()
}

func (s *SecLogSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *SecLogSuite) TestString(c *C) {
	levels := []seclog.Level{
		seclog.LevelDebug - 1,
		seclog.LevelDebug,
		seclog.LevelInfo,
		seclog.LevelWarn,
		seclog.LevelError,
		seclog.LevelError + 1,
		seclog.LevelCritical,
		seclog.LevelCritical + 2,
	}

	expected := []string{
		"DEBUG-1",
		"DEBUG",
		"INFO",
		"WARN",
		"ERROR",
		"CRITICAL",
		"CRITICAL",
		"CRITICAL+2",
	}

	c.Assert(len(levels), Equals, len(expected))

	obtained := make([]string, 0, len(levels))

	for _, level := range levels {
		obtained = append(obtained, level.String())
	}

	c.Assert(expected, DeepEquals, obtained)
}

func (s *SecLogSuite) TestSyslogPriority(c *C) {
	tests := []struct {
		level    seclog.Level
		expected syslog.Priority
	}{
		{seclog.LevelDebug - 1, syslog.LOG_DEBUG},
		{seclog.LevelDebug, syslog.LOG_DEBUG},
		{seclog.LevelInfo, syslog.LOG_INFO},
		{seclog.LevelWarn, syslog.LOG_WARNING},
		{seclog.LevelError, syslog.LOG_ERR},
		{seclog.LevelCritical, syslog.LOG_CRIT},
		{seclog.LevelCritical + 1, syslog.LOG_CRIT},
	}
	for _, t := range tests {
		c.Check(seclog.SyslogPriority(t.level), Equals, t.expected,
			Commentf("level %v", t.level))
	}
}

func (s *SecLogSuite) TestRegister(c *C) {
	restore := seclog.MockProviders(map[seclog.Impl]seclog.Provider{})
	defer restore()

	seclog.Register(seclog.SlogProvider{})

	// registering the same implementation again panics
	c.Assert(func() { seclog.Register(seclog.SlogProvider{}) }, PanicMatches,
		`attempting registration for existing logger "slog"`)
}

func (s *SecLogSuite) TestSetupUnknownImpl(c *C) {
	restore := seclog.MockProviders(map[seclog.Impl]seclog.Provider{})
	defer restore()

	err := seclog.Setup("unknown", seclog.SinkJournal, s.appID, seclog.LevelInfo)
	c.Assert(err, ErrorMatches,
		`cannot set up security logger: unknown implementation "unknown"`)
}

func (s *SecLogSuite) TestSetupSinkError(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return nil, fmt.Errorf("journal unavailable")
	})
	defer restore()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkJournal, s.appID, seclog.LevelInfo)
	c.Assert(err, ErrorMatches, "security logger disabled")
}

func (s *SecLogSuite) TestSetupSuccess(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		c.Check(appID, Equals, s.appID)
		return s.buf, nil
	})
	defer restore()

	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkJournal, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)

	// verify the logger is functional by logging through it
	seclog.LogLoginSuccess(seclog.SnapdUser{ID: 1, SystemUserName: "testuser"})
	c.Check(s.buf.Len() > 0, Equals, true)
}

func (s *SecLogSuite) setupSlogLogger(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return s.buf, nil
	})
	s.AddCleanup(restore)

	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	s.AddCleanup(restoreLogger)

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkJournal, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)
}

func (s *SecLogSuite) TestLogLoginSuccess(c *C) {
	s.setupSlogLogger(c)

	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@example.com",
		SystemUserName: "jdoe",
	}
	seclog.LogLoginSuccess(user)

	var obtained map[string]any
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained["level"], Equals, "INFO")
	c.Check(obtained["description"], Equals,
		"User 42:user@example.com:jdoe login success")
	c.Check(obtained["app_id"], Equals, s.appID)
	c.Check(obtained["category"], Equals, "AUTHN")
	c.Check(obtained["event"], Equals, "authn_login_success")
	userMap, ok := obtained["user"].(map[string]any)
	c.Assert(ok, Equals, true)
	c.Check(userMap["snapd-user-id"], Equals, float64(42))
	c.Check(userMap["store-user-email"], Equals, "user@example.com")
	c.Check(userMap["system-user-name"], Equals, "jdoe")
	c.Check(obtained["type"], Equals, "security")
}

func (s *SecLogSuite) TestLogLoginFailure(c *C) {
	s.setupSlogLogger(c)

	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@example.com",
		SystemUserName: "jdoe",
	}
	seclog.LogLoginFailure(user)

	var obtained map[string]any
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained["level"], Equals, "WARN")
	c.Check(obtained["description"], Equals,
		"User 42:user@example.com:jdoe login failure")
	c.Check(obtained["app_id"], Equals, s.appID)
	c.Check(obtained["category"], Equals, "AUTHN")
	c.Check(obtained["event"], Equals, "authn_login_failure")
	userMap, ok := obtained["user"].(map[string]any)
	c.Assert(ok, Equals, true)
	c.Check(userMap["snapd-user-id"], Equals, float64(42))
	c.Check(userMap["store-user-email"], Equals, "user@example.com")
	c.Check(userMap["system-user-name"], Equals, "jdoe")
	c.Check(obtained["type"], Equals, "security")
}

// closeTracker is a test helper that records whether Close was called.
type closeTracker struct {
	closed bool
	err    error
}

func (ct *closeTracker) Close() error {
	ct.closed = true
	return ct.err
}

func (s *SecLogSuite) TestDisableClosesTheSink(c *C) {
	tracker := &closeTracker{}
	restoreCloser := seclog.MockGlobalCloser(tracker)
	defer restoreCloser()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()
	restoreSetup := seclog.MockGlobalSetup(
		seclog.NewLoggerSetup(seclog.ImplSlog, seclog.SinkJournal, s.appID, seclog.LevelInfo))
	defer restoreSetup()

	err := seclog.Disable()
	c.Assert(err, IsNil)
	c.Check(tracker.closed, Equals, true)
}

func (s *SecLogSuite) TestDisableWithNoSinkReturnsNil(c *C) {
	restoreCloser := seclog.MockGlobalCloser(nil)
	defer restoreCloser()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	err := seclog.Disable()
	c.Assert(err, IsNil)
}

func (s *SecLogSuite) TestDisableIsIdempotent(c *C) {
	tracker := &closeTracker{}
	restoreCloser := seclog.MockGlobalCloser(tracker)
	defer restoreCloser()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()
	restoreSetup := seclog.MockGlobalSetup(
		seclog.NewLoggerSetup(seclog.ImplSlog, seclog.SinkJournal, s.appID, seclog.LevelInfo))
	defer restoreSetup()

	err := seclog.Disable()
	c.Assert(err, IsNil)
	c.Check(tracker.closed, Equals, true)

	// second call does not error even though closer is now nil
	err = seclog.Disable()
	c.Assert(err, IsNil)
}

func (s *SecLogSuite) TestDisablePropagatesError(c *C) {
	tracker := &closeTracker{err: fmt.Errorf("disk full")}
	restoreCloser := seclog.MockGlobalCloser(tracker)
	defer restoreCloser()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()
	restoreSetup := seclog.MockGlobalSetup(
		seclog.NewLoggerSetup(seclog.ImplSlog, seclog.SinkJournal, s.appID, seclog.LevelInfo))
	defer restoreSetup()

	err := seclog.Disable()
	c.Assert(err, ErrorMatches, "disk full")
}

// writeCloseTracker is a test helper that implements io.WriteCloser and
// records whether Close was called.
type writeCloseTracker struct {
	bytes.Buffer
	closed bool
}

func (wc *writeCloseTracker) Close() error {
	wc.closed = true
	return nil
}

func (s *SecLogSuite) TestSetupClosesPreviousSink(c *C) {
	first := &writeCloseTracker{}
	second := &writeCloseTracker{}
	call := 0
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		call++
		if call == 1 {
			return first, nil
		}
		return second, nil
	})
	defer restore()
	restoreCloser := seclog.MockGlobalCloser(nil)
	defer restoreCloser()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	// first setup
	err := seclog.Setup(seclog.ImplSlog, seclog.SinkJournal, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)
	c.Check(first.closed, Equals, false)

	// second setup should close the first sink
	err = seclog.Setup(seclog.ImplSlog, seclog.SinkJournal, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)
	c.Check(first.closed, Equals, true)
	c.Check(second.closed, Equals, false)
}
