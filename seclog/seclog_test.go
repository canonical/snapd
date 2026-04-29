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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/seclog/seclogtest"
	"github.com/snapcore/snapd/testutil"
)

type SecLogSuite struct {
	testutil.BaseTest
	buf *bytes.Buffer
}

var _ = Suite(&SecLogSuite{})

func TestSecLog(t *testing.T) { TestingT(t) }

func (s *SecLogSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.buf = &bytes.Buffer{}
	// No cleanup of the global logger is needed: every suite that
	// uses seclog calls Setup in its own SetUpTest, replacing any
	// leftover logger from a previous suite.
	seclog.Setup(seclogtest.MockSecurityLogger(s.buf))
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
		"UNKNOWN(0)",
		"DEBUG",
		"INFO",
		"WARN",
		"ERROR",
		"CRITICAL",
		"CRITICAL",
		"UNKNOWN(7)",
	}

	obtained := make([]string, 0, len(levels))

	for _, level := range levels {
		obtained = append(obtained, level.String())
	}

	c.Assert(expected, DeepEquals, obtained)
}

func (s *SecLogSuite) TestSnapdUserString(c *C) {
	// All fields set.
	c.Check(seclog.SnapdUser{
		ID: 42, StoreUserEmail: "a@b.com", StoreUserName: "jdoe",
	}.String(), Equals, "42:a@b.com:jdoe")

	// All fields zero/empty — all "<unknown>".
	c.Check(seclog.SnapdUser{}.String(), Equals, "<unknown>:<unknown>:<unknown>")

	// Only ID set.
	c.Check(seclog.SnapdUser{ID: 7}.String(), Equals, "7:<unknown>:<unknown>")

	// Only email set.
	c.Check(seclog.SnapdUser{StoreUserEmail: "x@y.z"}.String(), Equals, "<unknown>:x@y.z:<unknown>")

	// Only username set.
	c.Check(seclog.SnapdUser{StoreUserName: "root"}.String(), Equals, "<unknown>:<unknown>:root")
}

func (s *SecLogSuite) TestReasonString(c *C) {
	// Both fields set.
	c.Check(seclog.Reason{
		Code: seclog.ReasonInvalidCredentials, Message: "bad password",
	}.String(), Equals, "invalid-credentials:bad password")

	// Both fields empty — all "<unknown>".
	c.Check(seclog.Reason{}.String(), Equals, "<unknown>:<unknown>")

	// Only code set.
	c.Check(seclog.Reason{Code: seclog.ReasonInternal}.String(), Equals, "internal:<unknown>")

	// Only message set.
	c.Check(seclog.Reason{Message: "something broke"}.String(), Equals, "<unknown>:something broke")
}

func (s *SecLogSuite) TestSetupSuccess(c *C) {
	seclog.LogLoginSuccess(seclog.SnapdUser{ID: 1, StoreUserName: "testuser"})
	c.Check(s.buf.Len() > 0, Equals, true)
}

func (s *SecLogSuite) TestSetupReplacesExistingLogger(c *C) {
	// The first logger was set up in SetUpTest; verify it receives events.
	seclog.LogLoginSuccess(seclog.SnapdUser{ID: 1, StoreUserName: "first"})
	c.Check(s.buf.String(), testutil.Contains, "authn_login_success")

	// Replace with a second logger.
	secondBuf := &bytes.Buffer{}
	seclog.Setup(seclogtest.MockSecurityLogger(secondBuf))

	// New events go to the second logger, not the first.
	s.buf.Reset()
	seclog.LogLoginSuccess(seclog.SnapdUser{ID: 2, StoreUserName: "second"})
	c.Check(secondBuf.String(), testutil.Contains, "authn_login_success")
	c.Check(s.buf.Len(), Equals, 0)
}

func (s *SecLogSuite) TestLogLoginSuccess(c *C) {
	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@example.com",
		StoreUserName:  "jdoe",
	}
	seclog.LogLoginSuccess(user)

	c.Check(s.buf.String(), testutil.Contains, "authn_login_success")
	c.Check(s.buf.String(), testutil.Contains, "user@example.com")
}

func (s *SecLogSuite) TestLogLoginFailure(c *C) {
	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@example.com",
		StoreUserName:  "jdoe",
	}
	seclog.LogLoginFailure(user, seclog.Reason{Code: seclog.ReasonInvalidCredentials, Message: "invalid credentials"})

	c.Check(s.buf.String(), testutil.Contains, "authn_login_failure")
	c.Check(s.buf.String(), testutil.Contains, "user@example.com")
	c.Check(s.buf.String(), testutil.Contains, seclog.ReasonInvalidCredentials)
}

func (s *SecLogSuite) TestLogLoggerEnabledLogsEvent(c *C) {
	seclog.LogLoggerEnabled()

	c.Check(s.buf.String(), testutil.Contains, "sys_logging_enabled")
}

func (s *SecLogSuite) TestLogLoggerEnabledLogsToStandardLogger(c *C) {
	logBuf, restore := logger.MockLogger()
	defer restore()

	seclog.LogLoggerEnabled()

	c.Check(logBuf.String(), testutil.Contains, "security logger enabled")
}

func (s *SecLogSuite) TestLogLoggerDisabledLogsEvent(c *C) {
	seclog.LogLoggerDisabled()

	c.Check(s.buf.String(), testutil.Contains, "sys_logging_disabled")
}

func (s *SecLogSuite) TestLogLoggerDisabledLogsToStandardLogger(c *C) {
	logBuf, restore := logger.MockLogger()
	defer restore()

	seclog.LogLoggerDisabled()

	c.Check(logBuf.String(), testutil.Contains, "security logger disabled")
}
