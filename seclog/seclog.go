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
	"sync"

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

// String returns a name for the level.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelCritical:
		return "CRITICAL"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int(l))
	}
}

// Event describes a structured security audit event.
//
// A Version field may be added in the future if a log aggregator or
// consumer requires explicit schema versioning.
type Event struct {
	Category string `json:"category"`
	Name     string `json:"event"`
	Level    Level  `json:"level"`
}

// Attr is a key-value pair attached to a security log event.
type Attr struct {
	Key   string
	Value any
}

// SecurityLogger defines the interface for emitting structured security
// audit events. Implementations receive a fully described [Event] and
// optional [Attr] values, so new event types can be added without
// changing the interface.
type SecurityLogger interface {
	LogEvent(event Event, description string, attrs ...Attr)
}

var (
	globalLogger SecurityLogger = NewNopLogger()
	// lock guards globalLogger reads and writes.
	lock sync.Mutex
)

// Setup activates the security logger, replacing any previously
// configured logger.
//
// Setup is intended to be called once during early initialization.
func Setup(l SecurityLogger) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger = l
}

// LogLoggerEnabled logs that the security logger has been enabled.
func LogLoggerEnabled() {
	lock.Lock()
	defer lock.Unlock()

	if _, ok := globalLogger.(nopLogger); !ok {
		logger.Noticef("security logger enabled")
	}

	globalLogger.LogEvent(
		Event{Category: "SYS", Name: "sys_logging_enabled", Level: LevelInfo},
		"Security logging enabled",
	)
}

// LogLoggerDisabled logs that the security logger has been disabled.
func LogLoggerDisabled() {
	lock.Lock()
	defer lock.Unlock()

	if _, ok := globalLogger.(nopLogger); !ok {
		logger.Noticef("security logger disabled")
	}

	globalLogger.LogEvent(
		Event{Category: "SYS", Name: "sys_logging_disabled", Level: LevelCritical},
		"Security logging disabled",
	)
}

// LogLoginSuccess logs a successful login using the global security logger.
func LogLoginSuccess(user SnapdUser) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogEvent(
		Event{Category: "AUTHN", Name: "authn_login_success", Level: LevelInfo},
		fmt.Sprintf("User %s login success", user.String()),
		Attr{Key: "user", Value: user},
	)
}

// LogLoginFailure logs a failed login attempt using the global security logger.
func LogLoginFailure(user SnapdUser, reason Reason) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogEvent(
		Event{Category: "AUTHN", Name: "authn_login_failure", Level: LevelWarn},
		fmt.Sprintf("User %s login failure: %s", user.String(), reason.String()),
		Attr{Key: "user", Value: user},
		Attr{Key: "error", Value: reason},
	)
}

// LogTokenCreated logs a local snapd macaroon creation event using the
// global security logger.
func LogTokenCreated(user SnapdUser, tokenID int) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogEvent(
		Event{Category: "AUTHN", Name: "authn_token_created", Level: LevelInfo},
		fmt.Sprintf("Token created for user %s", user.String()),
		Attr{Key: "user", Value: user},
		Attr{Key: "token_id", Value: tokenID},
	)
}

// LogTokenDeleted logs a local snapd macaroon deletion event using the
// global security logger.
func LogTokenDeleted(user SnapdUser, tokenID int) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogEvent(
		Event{Category: "AUTHN", Name: "authn_token_delete", Level: LevelInfo},
		fmt.Sprintf("Token deleted for user %s", user.String()),
		Attr{Key: "user", Value: user},
		Attr{Key: "token_id", Value: tokenID},
	)
}

// LogUserCreated logs a user creation event using the global security logger.
func LogUserCreated(user SnapdUser) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogEvent(
		Event{Category: "USER", Name: "user_created", Level: LevelInfo},
		fmt.Sprintf("Created user %s", user.String()),
		Attr{Key: "user", Value: user},
	)
}

// LogUserUpdated logs a user update event using the global security logger.
func LogUserUpdated(user SnapdUser, changedFields []string) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogEvent(
		Event{Category: "USER", Name: "user_updated", Level: LevelInfo},
		fmt.Sprintf("Updated user %s", user.String()),
		Attr{Key: "user", Value: user},
		Attr{Key: "changed_fields", Value: changedFields},
	)
}

// LogUserRemoved logs a user removal event using the global security logger.
func LogUserRemoved(user SnapdUser) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogEvent(
		Event{Category: "USER", Name: "user_removed", Level: LevelInfo},
		fmt.Sprintf("Removed user %s", user.String()),
		Attr{Key: "user", Value: user},
	)
}

// LogAdminActivity logs an administrative API access event using the
// global security logger. It is emitted when authorization succeeds (the
// access gate passed), not when the API operation or handler succeeds.
func LogAdminActivity(user SnapdUser, peer Peer, endpoint Endpoint, checks AuthzChecks) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogEvent(
		Event{Category: "AUTHZ", Name: "authz_admin", Level: LevelInfo},
		fmt.Sprintf("User %s from %s accessed %s", user.String(), peer.String(), endpoint.String()),
		Attr{Key: "user", Value: user},
		Attr{Key: "peer", Value: peer},
		Attr{Key: "endpoint", Value: endpoint},
		Attr{Key: "authz_checks", Value: checks},
	)
}

// LogUnauthorizedAccess logs an unauthorized access attempt using the
// global security logger. It is emitted when authorization fails (the
// access gate denied the request), not when the API operation or handler
// fails after access was granted.
func LogUnauthorizedAccess(user SnapdUser, peer Peer, endpoint Endpoint, checks AuthzChecks, reason Reason) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogEvent(
		Event{Category: "AUTHZ", Name: "authz_fail", Level: LevelCritical},
		fmt.Sprintf("User %s from %s attempted to access %s without authorization: %s",
			user.String(), peer.String(), endpoint.String(), reason.String()),
		Attr{Key: "user", Value: user},
		Attr{Key: "peer", Value: peer},
		Attr{Key: "endpoint", Value: endpoint},
		Attr{Key: "authz_checks", Value: checks},
		Attr{Key: "error", Value: reason},
	)
}
