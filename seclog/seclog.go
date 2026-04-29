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
	"time"

	"github.com/snapcore/snapd/logger"
)

// unknown is the placeholder for empty fields in descriptions.
const unknown = "<unknown>"

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

// SnapdUser represents the identity of a user for security log events.
type SnapdUser struct {
	ID             int64     `json:"snapd-user-id"`
	StoreUserName  string    `json:"store-user-name"`
	StoreUserEmail string    `json:"store-user-email"`
	Expiration     time.Time `json:"expiration"`
}

// String returns a colon-separated description of the user in the form
// "<ID>:<StoreUserEmail>:<StoreUserName>". Fields that are unset use
// "<unknown>" as a placeholder. A zero ID means unset.
func (u SnapdUser) String() string {
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
	LogAny(event Event, description string, attrs ...Attr)
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
	logger.Noticef("security logger enabled")

	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogAny(
		Event{Category: "SYS", Name: "sys_logging_enabled", Level: LevelInfo},
		"Security logging enabled",
	)
}

// LogLoggerDisabled logs that the security logger has been disabled.
func LogLoggerDisabled() {
	logger.Noticef("security logger disabled")

	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogAny(
		Event{Category: "SYS", Name: "sys_logging_disabled", Level: LevelCritical},
		"Security logging disabled",
	)
}

// LogLoginSuccess logs a successful login using the global security logger.
func LogLoginSuccess(user SnapdUser) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogAny(
		Event{Category: "AUTHN", Name: "authn_login_success", Level: LevelInfo},
		fmt.Sprintf("User %s login success", user.String()),
		Attr{Key: "user", Value: user},
	)
}

// LogLoginFailure logs a failed login attempt using the global security logger.
func LogLoginFailure(user SnapdUser, reason Reason) {
	lock.Lock()
	defer lock.Unlock()

	globalLogger.LogAny(
		Event{Category: "AUTHN", Name: "authn_login_failure", Level: LevelWarn},
		fmt.Sprintf("User %s login failure: %s", user.String(), reason.String()),
		Attr{Key: "user", Value: user},
		Attr{Key: "error", Value: reason},
	)
}
