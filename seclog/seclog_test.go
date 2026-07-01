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
	seclog.Setup(seclogtest.MockSecurityLogger(s.buf))
	s.AddCleanup(func() { seclog.Setup(seclog.NewNopLogger()) })
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
	seclog.LogLoginFailure(user, seclog.Reason{Code: 401, Kind: "invalid-credentials", Message: "invalid credentials"})

	c.Check(s.buf.String(), testutil.Contains, "authn_login_failure")
	c.Check(s.buf.String(), testutil.Contains, "user@example.com")
	c.Check(s.buf.String(), testutil.Contains, "invalid-credentials")
}

func (s *SecLogSuite) TestLogAuthnTokenCreated(c *C) {
	user := seclog.SnapdUser{
		ID:             7,
		StoreUserEmail: "user@example.com",
		StoreUserName:  "jdoe",
	}
	seclog.LogAuthnTokenCreated(user, 99)

	c.Check(s.buf.String(), testutil.Contains, "authn_token_created")
	c.Check(s.buf.String(), testutil.Contains, "Token created for user 7:user@example.com:jdoe")
	c.Check(s.buf.String(), testutil.Contains, "[token_id=99]")
}

func (s *SecLogSuite) TestLogAuthnTokenDeleted(c *C) {
	user := seclog.SnapdUser{
		ID:             7,
		StoreUserEmail: "user@example.com",
		StoreUserName:  "jdoe",
	}
	seclog.LogAuthnTokenDeleted(user, 99)

	c.Check(s.buf.String(), testutil.Contains, "authn_token_delete")
	c.Check(s.buf.String(), testutil.Contains, "Token deleted for user 7:user@example.com:jdoe")
	c.Check(s.buf.String(), testutil.Contains, "[token_id=99]")
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

func (s *SecLogSuite) TestLogLoggerEnabledNopSkipsNoticef(c *C) {
	logBuf, restore := logger.MockLogger()
	defer restore()

	seclog.Setup(seclog.NewNopLogger())
	seclog.LogLoggerEnabled()

	c.Check(logBuf.String(), Not(testutil.Contains), "security logger enabled")
}

func (s *SecLogSuite) TestLogLoggerDisabledNopSkipsNoticef(c *C) {
	logBuf, restore := logger.MockLogger()
	defer restore()

	seclog.Setup(seclog.NewNopLogger())
	seclog.LogLoggerDisabled()

	c.Check(logBuf.String(), Not(testutil.Contains), "security logger disabled")
}

func (s *SecLogSuite) TestLogUserCreated(c *C) {
	user := seclog.SnapdUser{
		ID:             1,
		StoreUserEmail: "jdoe@test.com",
		StoreUserName:  "jdoe",
	}
	seclog.LogUserCreated(user)

	c.Check(s.buf.String(), testutil.Contains, "user_created")
	c.Check(s.buf.String(), testutil.Contains, "jdoe")
	c.Check(s.buf.String(), testutil.Contains, "jdoe@test.com")
}

func (s *SecLogSuite) TestLogUserUpdated(c *C) {
	user := seclog.SnapdUser{
		ID:             1,
		StoreUserEmail: "new@test.com",
		StoreUserName:  "jdoe",
	}
	seclog.LogUserUpdated(user, []string{"email", "store-macaroon"})

	c.Check(s.buf.String(), testutil.Contains, "user_updated")
	c.Check(s.buf.String(), testutil.Contains, "jdoe")
	c.Check(s.buf.String(), testutil.Contains, "new@test.com")
	c.Check(s.buf.String(), testutil.Contains, `[changed_fields=[]string{"email", "store-macaroon"}]`)
}

func (s *SecLogSuite) TestLogUserRemoved(c *C) {
	user := seclog.SnapdUser{
		ID:             1,
		StoreUserEmail: "jdoe@test.com",
		StoreUserName:  "jdoe",
	}
	seclog.LogUserRemoved(user)

	c.Check(s.buf.String(), testutil.Contains, "user_removed")
	c.Check(s.buf.String(), testutil.Contains, "jdoe")
	c.Check(s.buf.String(), testutil.Contains, "jdoe@test.com")
}

func (s *SecLogSuite) TestLogSystemUserCreated(c *C) {
	opts := seclog.AddOptions{
		RealUserName:        "Karl Popper",
		Sudoer:              true,
		ExtraUsers:          true,
		ForcePasswordChange: false,
		Known:               false,
	}
	seclog.LogSystemUserCreated("karl", opts, seclog.AddReasonAPICreateUserFromStoreCredentials)

	c.Check(s.buf.String(), testutil.Contains, "user_created_system")
	c.Check(s.buf.String(), testutil.Contains, "Created system user karl")
	c.Check(s.buf.String(), testutil.Contains, "karl")
	c.Check(s.buf.String(), testutil.Contains, "Karl Popper")
	c.Check(s.buf.String(), testutil.Contains, "Sudoer:true")
	c.Check(s.buf.String(), testutil.Contains, "Known:false")
	c.Check(s.buf.String(), testutil.Contains, `add_reason="api-create-user-from-store-credentials"`)
}

func (s *SecLogSuite) TestLogSystemUserCreatedWithAssertion(c *C) {
	opts := seclog.AddOptions{
		Known: true,
		Assertion: &seclog.Ref{
			Type:       "system-user",
			PrimaryKey: []string{"my-brand", "foo@bar.com"},
			Revision:   0,
		},
	}
	seclog.LogSystemUserCreated("example-user", opts, seclog.AddReasonAPICreateUserFromAssertion)

	c.Check(s.buf.String(), testutil.Contains, "user_created_system")
	c.Check(s.buf.String(), testutil.Contains, "system-user")
	c.Check(s.buf.String(), testutil.Contains, "my-brand")
	c.Check(s.buf.String(), testutil.Contains, "foo@bar.com")
	c.Check(s.buf.String(), testutil.Contains, "Known:true")
	c.Check(s.buf.String(), testutil.Contains, `add_reason="api-create-user-from-assertion"`)
}

func (s *SecLogSuite) TestLogSystemUserRemoved(c *C) {
	opts := seclog.RemoveOptions{Force: true}
	seclog.LogSystemUserRemoved("some-user", opts, seclog.RemoveReasonEnsureRemoveExpiredUser)

	c.Check(s.buf.String(), testutil.Contains, "user_removed_system")
	c.Check(s.buf.String(), testutil.Contains, "Removed system user some-user")
	c.Check(s.buf.String(), testutil.Contains, "some-user")
	c.Check(s.buf.String(), testutil.Contains, "Force:true")
	c.Check(s.buf.String(), testutil.Contains, `remove_reason="ensure-remove-expired-user"`)
}

// TestLogAdminActivity verifies that LogAdminActivity emits the expected event and attributes.
func (s *SecLogSuite) TestLogAdminActivity(c *C) {
	user := seclog.SnapdUser{ID: 1, StoreUserEmail: "admin@example.com", StoreUserName: "admin"}
	peer := seclog.Peer{Socket: "/run/snapd.socket", UID: 0, PID: 4242}
	endpoint := seclog.Endpoint{
		Method:        "POST",
		Path:          "/v2/snaps",
		Action:        "install",
		AccessChecker: "authenticated",
		AccessLevel:   "authenticated",
	}
	checks := seclog.NewAuthzChecks()

	seclog.LogAdminActivity(user, peer, endpoint, checks)

	c.Check(s.buf.String(), testutil.Contains, "authz_admin")
	c.Check(s.buf.String(), testutil.Contains, "from /run/snapd.socket")
	c.Check(s.buf.String(), testutil.Contains, "accessed POST:/v2/snaps:install")
	c.Check(s.buf.String(), testutil.Contains, "admin@example.com")
	c.Check(s.buf.String(), testutil.Contains, "/run/snapd.socket")
	c.Check(s.buf.String(), testutil.Contains, "4242")
	c.Check(s.buf.String(), testutil.Contains, "[peer=")
	c.Check(s.buf.String(), testutil.Contains, "[endpoint=")
	c.Check(s.buf.String(), testutil.Contains, "[authz_checks=")
	c.Check(s.buf.String(), testutil.Contains, "[user=")
}

// TestLogUnauthorizedAccess verifies that LogUnauthorizedAccess emits the expected event, peer, and reason.
func (s *SecLogSuite) TestLogUnauthorizedAccess(c *C) {
	user := seclog.SnapdUser{ID: 1, StoreUserEmail: "hacker@example.com", StoreUserName: "hacker"}
	peer := seclog.Peer{Socket: "/run/snapd.socket", UID: 1000, PID: 12345}
	endpoint := seclog.Endpoint{Method: "DELETE", Path: "/v2/snaps/core"}
	checks := seclog.NewAuthzChecks()
	reason := seclog.Reason{Code: 401, Kind: "invalid-credentials", Message: "no permission"}

	seclog.LogUnauthorizedAccess(user, peer, endpoint, checks, reason)

	c.Check(s.buf.String(), testutil.Contains, "authz_fail")
	c.Check(s.buf.String(), testutil.Contains, "from /run/snapd.socket")
	c.Check(s.buf.String(), testutil.Contains, "without authorization:")
	c.Check(s.buf.String(), testutil.Contains, "without authorization: 401:no permission")
	c.Check(s.buf.String(), testutil.Contains, "hacker@example.com")
	c.Check(s.buf.String(), testutil.Contains, "DELETE:/v2/snaps/core:<none>")
	c.Check(s.buf.String(), testutil.Contains, "12345")
	c.Check(s.buf.String(), testutil.Contains, "[error=")
	c.Check(s.buf.String(), testutil.Contains, "[user=")
}
