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

package seclog

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/snapcore/snapd/logger"
)

// Level is the importance or severity of a log event.
// The higher the level, the more severe the event.
type Level int

// Log levels.
const (
	LevelDebug    Level = 1
	LevelInfo     Level = 2
	LevelWarn     Level = 3
	LevelError    Level = 4
	LevelCritical Level = 5
)

// String returns a name for the level. If the level has a name, then that name
// in uppercase is returned. If the level is between named values, then an
// integer is appended to the uppercased name.
func (l Level) String() string {
	str := func(base string, val Level) string {
		if val == 0 {
			return base
		}
		return fmt.Sprintf("%s%+d", base, val)
	}

	switch {
	case l < LevelInfo:
		return str("DEBUG", l-LevelDebug)
	case l < LevelWarn:
		return str("INFO", l-LevelInfo)
	case l < LevelError:
		return str("WARN", l-LevelWarn)
	case l < LevelCritical:
		return str("ERROR", l-LevelError)
	default:
		return str("CRITICAL", l-LevelCritical)
	}
}

// Impl represents a known logger implementation identifier used for
// registration and selection of security loggers.
type Impl string

// Logger implementations.
const (
	ImplSlog Impl = "slog" // slog based structured logger
)

// Sink identifies a log output destination used for
// registration and selection of security log sinks.
type Sink string

// Sink types.
const (
	SinkAudit Sink = "audit" // kernel audit via netlink
)

// SnapdUser represents the identity of a user for security log events.
// The slog output schema is defined by [SnapdUser.LogValue], which
// renders Expiration as "never" for zero values instead of emitting a
// zero-value datetime.
type SnapdUser struct {
	ID             int64     `json:"snapd-user-id"`
	StoreUserName  string    `json:"store-user-name"`
	StoreUserEmail string    `json:"store-user-email"`
	Expiration     time.Time `json:"expiration"`
}

// String returns a colon-separated description of the user in the form
// "<ID>:<StoreUserEmail>:<StoreUserName>". Fields that are unset use
// "unknown" as a placeholder. A zero ID is treated as unset.
func (u SnapdUser) String() string {
	const unknown = "unknown"

	id := unknown
	if u.ID != 0 {
		id = fmt.Sprintf("%d", u.ID)
	}

	email := unknown
	if u.StoreUserEmail != "" {
		email = u.StoreUserEmail
	}

	name := unknown
	if u.StoreUserName != "" {
		name = u.StoreUserName
	}

	return id + ":" + email + ":" + name
}

// Reason codes are stable identifiers for security audit events.
const (
	ReasonInvalidCredentials = "invalid-credentials"
	ReasonTwoFactorRequired  = "two-factor-required"
	ReasonTwoFactorFailed    = "two-factor-failed"
	ReasonInvalidAuthData    = "invalid-auth-data"
	ReasonPasswordPolicy     = "password-policy"
	ReasonInternal           = "internal"
)

// Reason describes why a security event happened.
type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// String returns a colon-separated representation in the form
// "<Code>:<Message>". Fields that are unset use "unknown" as a
// placeholder.
func (r Reason) String() string {
	const unknown = "unknown"

	code := unknown
	if r.Code != "" {
		code = r.Code
	}

	message := unknown
	if r.Message != "" {
		message = r.Message
	}

	return code + ":" + message
}

// securityLogger defines the interface for emitting structured security
// audit events. Implementations are created by an [implFactory] and write
// to a configured sink.
type securityLogger interface {
	// LogLoggingEnabled and LogLoggingDisabled are internal events
	// emitted automatically when the logger is enabled or disabled.
	LogLoggingEnabled()
	LogLoggingDisabled()
	LogLoginSuccess(user SnapdUser)
	LogLoginFailure(user SnapdUser, reason Reason)
}

// loggerSetup holds the configuration provided to Setup.
type loggerSetup struct {
	impl     Impl
	sink     Sink
	appID    string
	minLevel Level
}

// implFactory provides functions required for constructing a [securityLogger].
// It is intended for registration of available loggers.
type implFactory interface {
	// New creates a securityLogger that writes to writer. Messages with a
	// severity below minLevel are silently dropped.
	New(writer io.Writer, appID string, minLevel Level) securityLogger
}

// sinkFactory creates an [io.Writer] for a log output destination.
// The appID identifies the application opening the sink and may be
// used by implementations for tagging or routing.
//
// If the returned writer also implements [io.Closer], it will be closed
// automatically when the sink is replaced or disabled.
type sinkFactory interface {
	Open(appID string) (io.Writer, error)
}

var (
	implementations                = map[Impl]implFactory{}
	sinks                          = map[Sink]sinkFactory{}
	globalLogger    securityLogger = newNopLogger()
	globalCloser    io.Closer
	globalSetup     *loggerSetup
	writeFailures   int
	failed          bool
	lock            sync.Mutex
)

// maxWriteFailures is the number of consecutive write failures
// tolerated before the security logger enters the failed state and
// is automatically disabled.
const maxWriteFailures = 3

// Setup stores the logger configuration and attempts to enable the
// security logger immediately. If the log sink cannot be opened (e.g.
// because the sink is not yet available), the configuration is still
// stored and a non-fatal "security logger disabled" error is returned.
// A subsequent call to Enable will re-attempt activation.
//
// Although Setup is reentrant, it is intended to be called exactly
// once per application, typically during early initialization.
func Setup(impl Impl, sink Sink, appID string, minLevel Level) error {
	lock.Lock()
	defer lock.Unlock()

	if _, exists := implementations[impl]; !exists {
		return fmt.Errorf("cannot set up security logger: unknown implementation %q", string(impl))
	}

	if _, exists := sinks[sink]; !exists {
		return fmt.Errorf("cannot set up security logger: unknown sink %q", string(sink))
	}

	globalSetup = &loggerSetup{impl: impl, sink: sink, appID: appID, minLevel: minLevel}
	if err := enableLocked(); err != nil {
		return fmt.Errorf("security logger disabled: %v", err)
	}

	return nil
}

// Enable opens the security log sink using the configuration stored by Setup,
// activating the security logger. If the sink is already open, it is closed
// and re-opened, refreshing the connection to the sink. Returns an error if
// Setup has not been called or if the sink cannot be opened.
func Enable() error {
	lock.Lock()
	defer lock.Unlock()

	if globalSetup == nil {
		return fmt.Errorf("cannot enable security logger: setup has not been called")
	}
	return enableLocked()
}

// Disable closes the security log sink and resets the global logger to nop.
// The stored configuration is retained so that Enable can re-open the sink
// later. If Setup has not been called, Disable is a no-op. Returns an error
// if the sink cannot be closed.
func Disable() error {
	lock.Lock()
	defer lock.Unlock()

	if globalSetup == nil {
		return nil
	}
	globalLogger.LogLoggingDisabled()
	logger.Noticef("security logger disabled")
	return closeSinkLocked()
}

// LogLoginSuccess logs a successful login using the global security logger.
func LogLoginSuccess(user SnapdUser) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogLoginSuccess(user)
}

// LogLoginFailure logs a failed login attempt using the global security logger.
func LogLoginFailure(user SnapdUser, reason Reason) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogLoginFailure(user, reason)
}

// registerImpl makes a logger factory available by name. The registration
// pattern allows implementations to be conditionally compiled via build tags
// without requiring the core package to import them directly.
//
// Should be called from the init() of the implementation file.
func registerImpl(name Impl, factory implFactory) {
	lock.Lock()
	defer lock.Unlock()

	if _, exists := implementations[name]; exists {
		panic(fmt.Sprintf("attempting re-registration for existing logger %q", name))
	}
	implementations[name] = factory
}

// registerSink makes a sink factory available by name. The registration
// pattern allows sinks to be conditionally compiled via build tags without
// requiring the core package to import them directly.
//
// Should be called from the init() of the sink file.
func registerSink(name Sink, factory sinkFactory) {
	lock.Lock()
	defer lock.Unlock()

	if _, exists := sinks[name]; exists {
		panic(fmt.Sprintf("attempting re-registration for existing sink %q", name))
	}
	sinks[name] = factory
}

// enableLocked resolves the logger factory, opens the sink, and activates the
// logger. Must be called with lock held and globalSetup non-nil.
func enableLocked() error {
	factory, exists := implementations[globalSetup.impl]
	if !exists {
		return fmt.Errorf("internal error: implementation %q missing", string(globalSetup.impl))
	}

	newSink, exists := sinks[globalSetup.sink]
	if !exists {
		return fmt.Errorf("internal error: sink %q missing", string(globalSetup.sink))
	}

	writer, err := openSinkLocked(newSink, globalSetup.appID)
	if err != nil {
		return fmt.Errorf("cannot enable security logger: %v", err)
	}

	// Wrap the writer with failure tracking so that repeated write
	// errors automatically disable the logger.
	tracked := &failureTrackingWriter{
		writer:        writer,
		writeFailures: &writeFailures,
		failed:        &failed,
		maxFailures:   maxWriteFailures,
		onThresholdReached: func(failures int, lastErr error) {
			logger.Noticef("security logger failed after %d consecutive write errors, disabling (last error: %v)", failures, lastErr)
			closeSinkLocked()
		},
	}
	globalLogger = factory.New(tracked, globalSetup.appID, globalSetup.minLevel)
	writeFailures = 0
	failed = false
	globalLogger.LogLoggingEnabled()
	logger.Noticef("security logger enabled")
	return nil
}

// openSinkLocked opens the log sink and manages the closer. Any previously
// open sink is closed first. Must be called with lock held.
func openSinkLocked(factory sinkFactory, appID string) (io.Writer, error) {
	writer, err := factory.Open(appID)
	if err != nil {
		return nil, err
	}

	if globalCloser != nil {
		globalCloser.Close()
		globalCloser = nil
	}

	// If the writer also implements io.Closer, track it so
	// the sink is closed when replaced or disabled.
	if closer, ok := writer.(io.Closer); ok {
		globalCloser = closer
	}

	return writer, nil
}

// closeSinkLocked closes the security log sink and resets the global logger to
// nop. Must be called with lock held.
func closeSinkLocked() error {
	globalLogger = newNopLogger()
	if globalCloser != nil {
		err := globalCloser.Close()
		globalCloser = nil
		return err
	}
	return nil
}

// levelWriter extends [io.Writer] with per-message level control. Writers
// that implement this interface allow log handlers to set the severity for
// each message before writing.
//
// This interface is defined here rather than in slog.go so that
// [failureTrackingWriter] can implement it without a build-tag
// dependency on log/slog. The slog layer's [levelHandler] and the
// audit sink's [auditWriter] are the primary consumers.
type levelWriter interface {
	io.Writer
	SetLevel(Level)
}

// failureTrackingWriter wraps an [io.Writer] and counts consecutive
// write failures. When maxFailures consecutive errors are reached it
// invokes onThresholdReached and marks the logger as failed.
//
// All mutable state (writeFailures, failed) is injected via pointers
// so that the writer does not implicitly depend on package globals.
// The caller must hold [lock] when calling Write; since Write is
// invoked from within a locked Log* call, the lock is already held.
type failureTrackingWriter struct {
	writer             io.Writer
	writeFailures      *int
	failed             *bool
	maxFailures        int
	onThresholdReached func(failures int, lastErr error)
}

var _ levelWriter = (*failureTrackingWriter)(nil)

func (w *failureTrackingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err != nil {
		*w.writeFailures++
		if *w.writeFailures >= w.maxFailures && !*w.failed {
			*w.failed = true
			w.onThresholdReached(*w.writeFailures, err)
		}
		return n, err
	}
	*w.writeFailures = 0
	return n, nil
}

// SetLevel implements [levelWriter] so the tracking wrapper is
// transparent to the [levelHandler].
func (w *failureTrackingWriter) SetLevel(l Level) {
	if lw, ok := w.writer.(levelWriter); ok {
		lw.SetLevel(l)
	}
}
