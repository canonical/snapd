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

// This file contains the Go types that represent the data carried by security
// audit events, across all event categories. It is intentionally separate
// from seclog.go, which owns the emission machinery (SecurityLogger, Setup,
// LogEvent wrappers).
//
// Design goals:
//
//  1. Spec alignment: field names and JSON tags match the security audit
//     specification directly.
//
//  2. No imports from other snapd packages: seclog is imported by
//     packages such as overlord/auth, so it cannot import them back.
//     Types here must be self-contained. The translation from an
//     internal type (e.g. auth.UserState) to an audit event type is
//     the responsibility of the caller.
//
// When adding a new event category, define its types here.

package seclog

import (
	"fmt"
	"time"
)

// unknown is the placeholder for empty fields in descriptions.
const unknown = "<unknown>"

// Reason codes are stable identifiers for security audit events.
const (
	ReasonInvalidCredentials = "invalid-credentials"
	ReasonTwoFactorRequired  = "two-factor-required"
	ReasonTwoFactorFailed    = "two-factor-failed"
	ReasonInvalidAuthData    = "invalid-auth-data"
	ReasonPasswordPolicy     = "password-policy"
	ReasonInternal           = "internal"
)

// Reason describes why a security event happened. The JSON tags match
// the security audit specification field names.
type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// String returns a colon-separated representation in the form
// "<Code>:<Message>". Fields that are unset use "<unknown>" as a
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

// SnapdUser represents the identity of a user for security log events.
type SnapdUser struct {
	ID             int64     `json:"snapd-user-id"`
	StoreUserName  string    `json:"store-user-name"`
	StoreUserEmail string    `json:"store-user-email"`
	Expiration     time.Time `json:"expiration"`
}

// Endpoint describes an API endpoint involved in an authorization event.
type Endpoint struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Action      string `json:"action"`
	AccessCheck string `json:"access-check"`
}

// String returns a colon-separated representation in the form
// "<Method>:<Path>:<Action>".
func (e Endpoint) String() string {
	s := e.Method + ":" + e.Path
	if e.Action != "" {
		s += ":" + e.Action
	}
	return s
}

// AuthzCheck represents the outcome of a single authorization stage.
type AuthzCheck string

const (
	// AuthzPass indicates that the authorization stage passed.
	AuthzPass AuthzCheck = "pass"
	// AuthzFail indicates that the authorization stage failed.
	AuthzFail AuthzCheck = "fail"
	// AuthzNA indicates that the authorization stage was not applicable.
	AuthzNA AuthzCheck = "n/a"
)

// AuthzChecks captures the outcome of each authorization stage evaluated
// during an access check. Each field records whether that stage passed,
// failed, or was not applicable to the request.
type AuthzChecks struct {
	PeerCreds AuthzCheck `json:"peer-credentials-available"`
	Socket    AuthzCheck `json:"socket-allowed"`
	Interface AuthzCheck `json:"interface-requirements"`
	Open      AuthzCheck `json:"open-access"`
	UserAuth  AuthzCheck `json:"user-authentication"`
	Root      AuthzCheck `json:"root"`
	Polkit    AuthzCheck `json:"polkit"`
}

// NewAuthzChecks returns an AuthzChecks with all fields set to [AuthzNA].
func NewAuthzChecks() AuthzChecks {
	return AuthzChecks{
		PeerCreds: AuthzNA,
		Socket:    AuthzNA,
		Interface: AuthzNA,
		Open:      AuthzNA,
		UserAuth:  AuthzNA,
		Root:      AuthzNA,
		Polkit:    AuthzNA,
	}
}

// String returns a colon-separated description of the user in the form
// "<ID>:<StoreUserEmail>:<StoreUserName>". Fields that are unset use
// "<unknown>" as a placeholder; a zero ID is considered unset.
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
