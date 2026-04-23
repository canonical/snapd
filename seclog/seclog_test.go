// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build go1.21 && !noslog

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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
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

func (s *SecLogSuite) TestSnapdUserString(c *C) {
	// All fields set.
	c.Check(seclog.SnapdUser{
		ID: 42, StoreUserEmail: "a@b.com", StoreUserName: "jdoe",
	}.String(), Equals, "42:a@b.com:jdoe")

	// All fields zero/empty — all "unknown".
	c.Check(seclog.SnapdUser{}.String(), Equals, "unknown:unknown:unknown")

	// Only ID set.
	c.Check(seclog.SnapdUser{ID: 7}.String(), Equals, "7:unknown:unknown")

	// Only email set.
	c.Check(seclog.SnapdUser{StoreUserEmail: "x@y.z"}.String(), Equals, "unknown:x@y.z:unknown")

	// Only username set.
	c.Check(seclog.SnapdUser{StoreUserName: "root"}.String(), Equals, "unknown:unknown:root")
}

func (s *SecLogSuite) TestReasonString(c *C) {
	// Both fields set.
	c.Check(seclog.Reason{
		Code: seclog.ReasonInvalidCredentials, Message: "bad password",
	}.String(), Equals, "invalid-credentials:bad password")

	// Both fields empty — all "unknown".
	c.Check(seclog.Reason{}.String(), Equals, "unknown:unknown")

	// Only code set.
	c.Check(seclog.Reason{Code: seclog.ReasonInternal}.String(), Equals, "internal:unknown")

	// Only message set.
	c.Check(seclog.Reason{Message: "something broke"}.String(), Equals, "unknown:something broke")
}

func (s *SecLogSuite) TestRegisterImpl(c *C) {
	restore := seclog.MockImplementations(map[seclog.Impl]seclog.ImplFactory{})
	defer restore()

	seclog.RegisterImpl(seclog.ImplSlog, seclog.SlogImplementation{})

	// registering the same implementation again panics
	c.Assert(func() { seclog.RegisterImpl(seclog.ImplSlog, seclog.SlogImplementation{}) }, PanicMatches,
		`attempting re-registration for existing logger "slog"`)
}

func (s *SecLogSuite) TestRegisterSinkDuplicate(c *C) {
	restore := seclog.MockSinks(map[seclog.Sink]seclog.SinkFactory{})
	defer restore()

	dummy := seclog.SinkFunc(func(string) (io.Writer, error) { return nil, nil })
	seclog.RegisterSink(seclog.SinkAudit, dummy)

	// registering the same sink again panics
	c.Assert(func() { seclog.RegisterSink(seclog.SinkAudit, dummy) }, PanicMatches,
		`attempting re-registration for existing sink "audit"`)
}

func (s *SecLogSuite) TestSetupUnknownImpl(c *C) {
	restore := seclog.MockImplementations(map[seclog.Impl]seclog.ImplFactory{})
	defer restore()

	err := seclog.Setup("unknown", seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, ErrorMatches,
		`cannot set up security logger: unknown implementation "unknown"`)
}

func (s *SecLogSuite) TestSetupUnknownSink(c *C) {
	restore := seclog.MockSinks(map[seclog.Sink]seclog.SinkFactory{})
	defer restore()

	err := seclog.Setup(seclog.ImplSlog, "unknown", s.appID, seclog.LevelInfo)
	c.Assert(err, ErrorMatches,
		`cannot set up security logger: unknown sink "unknown"`)
}

func (s *SecLogSuite) TestSetupSinkError(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return nil, fmt.Errorf("journal unavailable")
	})
	defer restore()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, ErrorMatches, "security logger disabled: cannot enable security logger: journal unavailable")
}

func (s *SecLogSuite) TestSetupSuccess(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		c.Check(appID, Equals, s.appID)
		return s.buf, nil
	})
	defer restore()

	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)

	// verify the logger is functional by logging through it
	seclog.LogLoginSuccess(seclog.SnapdUser{ID: 1, StoreUserName: "testuser"})
	c.Check(s.buf.Len() > 0, Equals, true)
}

func (s *SecLogSuite) setupSlogLogger(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return s.buf, nil
	})
	s.AddCleanup(restore)

	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	s.AddCleanup(restoreLogger)

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)

	// Reset buffer after Setup, which logs the "logging enabled" event.
	s.buf.Reset()
}

func (s *SecLogSuite) TestLogLoginSuccess(c *C) {
	s.setupSlogLogger(c)

	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@example.com",
		StoreUserName:  "jdoe",
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
	c.Check(userMap["store-user-name"], Equals, "jdoe")
	c.Check(obtained["type"], Equals, "security")
}

func (s *SecLogSuite) TestLogLoginFailure(c *C) {
	s.setupSlogLogger(c)

	user := seclog.SnapdUser{
		ID:             42,
		StoreUserEmail: "user@example.com",
		StoreUserName:  "jdoe",
	}
	seclog.LogLoginFailure(user, seclog.Reason{Code: seclog.ReasonInvalidCredentials, Message: "invalid credentials"})

	var obtained map[string]any
	err := json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained["level"], Equals, "WARN")
	c.Check(obtained["description"], Equals,
		"User 42:user@example.com:jdoe login failure: invalid-credentials:invalid credentials")
	c.Check(obtained["app_id"], Equals, s.appID)
	c.Check(obtained["category"], Equals, "AUTHN")
	c.Check(obtained["event"], Equals, "authn_login_failure")
	userMap, ok := obtained["user"].(map[string]any)
	c.Assert(ok, Equals, true)
	c.Check(userMap["snapd-user-id"], Equals, float64(42))
	c.Check(userMap["store-user-email"], Equals, "user@example.com")
	c.Check(userMap["store-user-name"], Equals, "jdoe")
	errMap, ok := obtained["error"].(map[string]any)
	c.Assert(ok, Equals, true)
	c.Check(errMap["code"], Equals, seclog.ReasonInvalidCredentials)
	c.Check(errMap["message"], Equals, "invalid credentials")
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
		seclog.NewLoggerSetup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo))
	defer restoreSetup()

	err := seclog.Disable()
	c.Assert(err, IsNil)
	c.Check(tracker.closed, Equals, true)
}

func (s *SecLogSuite) TestDisableLogsDisabledEvent(c *C) {
	s.setupSlogLogger(c)

	err := seclog.Disable()
	c.Assert(err, IsNil)

	var obtained map[string]any
	err = json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained["level"], Equals, "CRITICAL")
	c.Check(obtained["description"], Equals, "Security logging disabled")
	c.Check(obtained["category"], Equals, "SYS")
	c.Check(obtained["event"], Equals, "sys_logging_disabled")
}

func (s *SecLogSuite) TestDisableWithNoSetupIsNoop(c *C) {
	restoreCloser := seclog.MockGlobalCloser(nil)
	defer restoreCloser()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()
	restoreSetup := seclog.MockGlobalSetup(nil)
	defer restoreSetup()

	err := seclog.Disable()
	c.Assert(err, IsNil)
}

func (s *SecLogSuite) TestEnableWithNoSetupReturnsError(c *C) {
	restoreSetup := seclog.MockGlobalSetup(nil)
	defer restoreSetup()

	err := seclog.Enable()
	c.Assert(err, ErrorMatches, "cannot enable security logger: setup has not been called")
}

func (s *SecLogSuite) TestEnableWithMissingImpl(c *C) {
	restoreSetup := seclog.MockGlobalSetup(
		seclog.NewLoggerSetup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo))
	defer restoreSetup()
	restoreImpls := seclog.MockImplementations(map[seclog.Impl]seclog.ImplFactory{})
	defer restoreImpls()

	err := seclog.Enable()
	c.Assert(err, ErrorMatches, `internal error: implementation "slog" missing`)
}

func (s *SecLogSuite) TestEnableWithMissingSink(c *C) {
	restoreSetup := seclog.MockGlobalSetup(
		seclog.NewLoggerSetup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo))
	defer restoreSetup()
	restoreSinks := seclog.MockSinks(map[seclog.Sink]seclog.SinkFactory{})
	defer restoreSinks()

	err := seclog.Enable()
	c.Assert(err, ErrorMatches, `internal error: sink "audit" missing`)
}

func (s *SecLogSuite) TestEnableAfterDisable(c *C) {
	s.setupSlogLogger(c)

	err := seclog.Disable()
	c.Assert(err, IsNil)
	s.buf.Reset()

	err = seclog.Enable()
	c.Assert(err, IsNil)
	s.buf.Reset()
	user := seclog.SnapdUser{
		ID:             1,
		StoreUserEmail: "a@b.com",
		StoreUserName:  "u",
	}
	seclog.LogLoginSuccess(user)

	var obtained map[string]any
	err = json.Unmarshal(s.buf.Bytes(), &obtained)
	c.Assert(err, IsNil)
	c.Check(obtained["event"], Equals, "authn_login_success")
}

func (s *SecLogSuite) TestDisableIsIdempotent(c *C) {
	tracker := &closeTracker{}
	restoreCloser := seclog.MockGlobalCloser(tracker)
	defer restoreCloser()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()
	restoreSetup := seclog.MockGlobalSetup(
		seclog.NewLoggerSetup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo))
	defer restoreSetup()

	err := seclog.Disable()
	c.Assert(err, IsNil)
	c.Check(tracker.closed, Equals, true)

	// second call does not error even though closer is now nil
	err = seclog.Disable()
	c.Assert(err, IsNil)
}

func (s *SecLogSuite) TestEnableIsIdempotent(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return s.buf, nil
	})
	defer restore()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)

	// second call does not error
	err = seclog.Enable()
	c.Assert(err, IsNil)

	// logger is still functional
	s.buf.Reset()
	seclog.LogLoginSuccess(seclog.SnapdUser{ID: 1, StoreUserName: "test"})
	c.Check(s.buf.Len() > 0, Equals, true)
}

func (s *SecLogSuite) TestDisablePropagatesError(c *C) {
	tracker := &closeTracker{err: fmt.Errorf("disk full")}
	restoreCloser := seclog.MockGlobalCloser(tracker)
	defer restoreCloser()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()
	restoreSetup := seclog.MockGlobalSetup(
		seclog.NewLoggerSetup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo))
	defer restoreSetup()

	err := seclog.Disable()
	c.Assert(err, ErrorMatches, "disk full")
}

func (s *SecLogSuite) TestEnablePropagatesError(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return nil, fmt.Errorf("sink unavailable")
	})
	defer restore()
	restoreSetup := seclog.MockGlobalSetup(
		seclog.NewLoggerSetup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo))
	defer restoreSetup()

	err := seclog.Enable()
	c.Assert(err, ErrorMatches, "cannot enable security logger: sink unavailable")
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
	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)
	c.Check(first.closed, Equals, false)

	// second setup should close the first sink
	err = seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)
	c.Check(first.closed, Equals, true)
	c.Check(second.closed, Equals, false)
}

// countingWriter counts successful writes before switching to errors.
type countingWriter struct {
	buf       bytes.Buffer
	successes int // number of remaining successful writes
}

func (w *countingWriter) Write(p []byte) (int, error) {
	if w.successes > 0 {
		w.successes--
		return w.buf.Write(p)
	}
	return 0, fmt.Errorf("write failed")
}

func (s *SecLogSuite) TestWriteFailuresDisableAfterThreshold(c *C) {
	// Allow LogLoggingEnabled to succeed so writeFailures starts at 0;
	// only the test loop writes trigger failures.
	cw := &countingWriter{successes: 1}
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return cw, nil
	})
	defer restore()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	logBuf, restoreStdLogger := logger.MockLogger()
	defer restoreStdLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)
	logBuf.Reset()

	user := seclog.SnapdUser{ID: 1, StoreUserName: "test"}

	// Exactly maxWriteFailures consecutive failures trigger auto-disable.
	for i := 0; i < seclog.MaxWriteFailures; i++ {
		seclog.LogLoginSuccess(user)
	}

	c.Check(seclog.GetFailed(), Equals, true)
	c.Check(seclog.GetWriteFailures(), Equals, seclog.MaxWriteFailures)
	c.Check(logBuf.String(), testutil.Contains,
		"security logger failed after 3 consecutive write errors, disabling")
}

func (s *SecLogSuite) TestWriteFailuresDoNotDisableBelowThreshold(c *C) {
	// Allow LogLoggingEnabled to succeed so writeFailures starts at 0.
	cw := &countingWriter{successes: 1}
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return cw, nil
	})
	defer restore()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)

	user := seclog.SnapdUser{ID: 1, StoreUserName: "test"}

	// Fewer than maxWriteFailures failures should not trigger auto-disable.
	for i := 0; i < seclog.MaxWriteFailures-1; i++ {
		seclog.LogLoginSuccess(user)
	}

	c.Check(seclog.GetFailed(), Equals, false)
	c.Check(seclog.GetWriteFailures(), Equals, seclog.MaxWriteFailures-1)
}

func (s *SecLogSuite) TestWriteSuccessResetsFailureCount(c *C) {
	cw := &countingWriter{successes: 100}
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return cw, nil
	})
	defer restore()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)

	// Simulate some failures below the threshold.
	restoreFailures := seclog.MockWriteFailures(seclog.MaxWriteFailures - 1)
	defer restoreFailures()

	user := seclog.SnapdUser{ID: 1, StoreUserName: "test"}
	// A successful write resets the counter.
	seclog.LogLoginSuccess(user)

	c.Check(seclog.GetWriteFailures(), Equals, 0)
	c.Check(seclog.GetFailed(), Equals, false)
}

func (s *SecLogSuite) TestEnableResetsFailureState(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return s.buf, nil
	})
	defer restore()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)

	// Simulate a failed state.
	restoreFailures := seclog.MockWriteFailures(seclog.MaxWriteFailures)
	defer restoreFailures()
	restoreFailed := seclog.MockFailed(true)
	defer restoreFailed()

	// Re-enable should reset the failure state.
	err = seclog.Enable()
	c.Assert(err, IsNil)
	c.Check(seclog.GetFailed(), Equals, false)
	c.Check(seclog.GetWriteFailures(), Equals, 0)
}

func (s *SecLogSuite) TestEnableLogsToStandardLogger(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return s.buf, nil
	})
	defer restore()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	logBuf, restoreStdLogger := logger.MockLogger()
	defer restoreStdLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)

	c.Check(logBuf.String(), testutil.Contains, "security logger enabled")
}

func (s *SecLogSuite) TestDisableLogsToStandardLogger(c *C) {
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return s.buf, nil
	})
	defer restore()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)

	logBuf, restoreStdLogger := logger.MockLogger()
	defer restoreStdLogger()

	err = seclog.Disable()
	c.Assert(err, IsNil)

	c.Check(logBuf.String(), testutil.Contains, "security logger disabled")
}

func (s *SecLogSuite) TestFailureTrackingWriterPassesSetLevel(c *C) {
	// Use a levelBuf (defined in slog_test.go) which implements
	// levelWriter so we can verify SetLevel is called through
	// the failureTrackingWriter wrapper.
	lb := &levelBuf{}
	restore := seclog.MockNewSink(func(appID string) (io.Writer, error) {
		return lb, nil
	})
	defer restore()
	restoreLogger := seclog.MockGlobalLogger(seclog.NewNopLogger())
	defer restoreLogger()

	err := seclog.Setup(seclog.ImplSlog, seclog.SinkAudit, s.appID, seclog.LevelInfo)
	c.Assert(err, IsNil)
	lb.Reset()
	lb.levels = nil

	seclog.LogLoginSuccess(seclog.SnapdUser{ID: 1, StoreUserName: "test"})

	// The levelHandler should have called SetLevel on the underlying
	// levelBuf through the failureTrackingWriter wrapper.
	c.Assert(len(lb.levels), Equals, 1)
	c.Check(lb.levels[0], Equals, seclog.LevelInfo)
}
