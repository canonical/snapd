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
)

var (
	providers                   = map[Impl]provider{}
	sinks                       = map[Sink]func(string) (io.Writer, error){}
	globalLogger securityLogger = newNopLogger()
	globalCloser io.Closer
	globalSetup  *loggerSetup
	lock         sync.Mutex
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

// String returns a name for the level.
// If the level has a name, then that name
// in uppercase is returned.
// If the level is between named values, then
// an integer is appended to the uppercased name.
// Examples:
//
//	LevelWarn.String() => "WARN"
//	(LevelCritical+2).String() => "CRITICAL+2"
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

// Sink identifies a log output destination.
type Sink string

// Sink types.
const (
	SinkJournal Sink = "journal" // journald namespace stream
	SinkAudit   Sink = "audit"   // kernel audit via netlink
)

// SnapdUser represents the identity of a user for security log events.
type SnapdUser struct {
	ID             int64     `json:"snapd-user-id"`
	SystemUserName string    `json:"system-user-name,omitempty"`
	StoreUserEmail string    `json:"store-user-email,omitempty"`
	Expiration     time.Time `json:"expiration,omitzero"`
}

// String returns a colon-separated description of the user in the form
// "<ID>:<StoreUserEmail>:<SystemUserName>". Fields that are unset use
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
	if u.SystemUserName != "" {
		name = u.SystemUserName
	}
	return id + ":" + email + ":" + name
}

// securityLogger defines the interface for emitting structured security
// audit events. Implementations are created by a [provider] and write
// to a configured sink.
type securityLogger interface {
	LogLoggingEnabled()
	LogLoggingDisabled()
	LogLoginSuccess(user SnapdUser)
	LogLoginFailure(user SnapdUser)
}

// loggerSetup holds the configuration provided to Setup.
type loggerSetup struct {
	impl     Impl
	sink     Sink
	appID    string
	minLevel Level
}

// provider provides functions required for constructing a [securityLogger].
// It is intended for registration of available loggers.
type provider interface {
	// New creates a securityLogger that writes to writer. Messages with a
	// severity below minLevel are silently dropped.
	New(writer io.Writer, appID string, minLevel Level) securityLogger
	// Impl returns the identifier for this provider.
	Impl() Impl
}

// Setup stores the logger configuration and attempts to enable the
// security logger immediately. If the log sink cannot be opened (e.g.
// because the journal namespace is not active yet), the configuration
// is still stored and a non-fatal "security logger disabled" error is
// returned. A subsequent call to Enable will re-attempt activation.
func Setup(impl Impl, sink Sink, appID string, minLevel Level) error {
	lock.Lock()
	defer lock.Unlock()

	if _, exists := providers[impl]; !exists {
		return fmt.Errorf("cannot set up security logger: unknown implementation %q", string(impl))
	}
	if _, exists := sinks[sink]; !exists {
		return fmt.Errorf("cannot set up security logger: unknown sink %q", string(sink))
	}
	globalSetup = &loggerSetup{impl: impl, sink: sink, appID: appID, minLevel: minLevel}
	if err := enableLocked(); err != nil {
		return fmt.Errorf("security logger disabled")
	}
	return nil
}

// Enable opens the security log sink using the configuration stored by Setup,
// activating the security logger. If the sink is already open, it is closed
// and re-opened, refreshing the connection to the journal namespace.
// Returns an error if Setup has not been called or if the sink cannot be opened.
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
// later. It is safe to call even if the logger is already a nop.
func Disable() error {
	lock.Lock()
	defer lock.Unlock()
	if globalSetup == nil {
		return nil
	}
	return closeSinkLocked()
}

// LogLoggingEnabled logs that security auditing has been enabled.
func LogLoggingEnabled() {
	lock.Lock()
	defer lock.Unlock()
	globalLogger.LogLoggingEnabled()
}

// LogLoggingDisabled logs that security auditing has been disabled.
func LogLoggingDisabled() {
	lock.Lock()
	defer lock.Unlock()
	globalLogger.LogLoggingDisabled()
}

// LogLoginSuccess logs a successful login using the global security logger.
func LogLoginSuccess(user SnapdUser) {
	lock.Lock()
	defer lock.Unlock()
	globalLogger.LogLoginSuccess(user)
}

// LogLoginFailure logs a failed login attempt using the global security logger.
func LogLoginFailure(user SnapdUser) {
	lock.Lock()
	defer lock.Unlock()
	globalLogger.LogLoginFailure(user)
}

// register makes a provider available by name.
// Should be called from init().
func register(p provider) {
	lock.Lock()
	defer lock.Unlock()
	impl := p.Impl()
	if _, exists := providers[impl]; exists {
		panic(fmt.Sprintf("attempting registration for existing logger %q", impl))
	}
	providers[impl] = p
}

// registerSink makes a sink factory available by name.
// Should be called from init().
func registerSink(name Sink, factory func(string) (io.Writer, error)) {
	lock.Lock()
	defer lock.Unlock()
	if _, exists := sinks[name]; exists {
		panic(fmt.Sprintf("attempting registration for existing sink %q", name))
	}
	sinks[name] = factory
}

// enableLocked resolves the provider, opens the sink, and activates the
// logger. Must be called with lock held and globalSetup non-nil.
func enableLocked() error {
	provider, exists := providers[globalSetup.impl]
	if !exists {
		return fmt.Errorf("internal error: provider %q missing", string(globalSetup.impl))
	}
	newSink, exists := sinks[globalSetup.sink]
	if !exists {
		return fmt.Errorf("internal error: sink %q missing", string(globalSetup.sink))
	}
	writer, err := openSinkLocked(newSink, globalSetup.appID)
	if err != nil {
		return fmt.Errorf("cannot enable security logger: %w", err)
	}
	globalLogger = provider.New(writer, globalSetup.appID, globalSetup.minLevel)
	return nil
}

// openSinkLocked opens the log sink and manages the closer. Any previously
// open sink is closed first. Must be called with lock held.
func openSinkLocked(newSink func(string) (io.Writer, error), appID string) (io.Writer, error) {
	writer, err := newSink(appID)
	if err != nil {
		return nil, err
	}
	if globalCloser != nil {
		globalCloser.Close()
		globalCloser = nil
	}
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
