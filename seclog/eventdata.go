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
// from seclog.go, which owns the emission machinery ([SecurityLogger], [Setup],
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
//     internal type (e.g. [auth.UserState]) to an audit event type is
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

// none indicates an endpoint has no action (e.g. non-POST requests).
const none = "<none>"

// Reason describes why a security event happened. The JSON tags match
// the security audit specification field names.
type Reason struct {
	// Code is a numeric error code defined by its originating domain:
	// an HTTP response code (e.g. 401, 500), a standard-library code,
	// or a custom code. Zero means unset.
	Code int `json:"code"`
	// Kind is an existing error-kind identifier from that domain (e.g.
	// "invalid-credentials"), for programmatic matching, not display.
	Kind string `json:"kind"`
	// Message is the human-readable explanation, suitable for logs.
	Message string `json:"message"`
}

// String returns a colon-separated representation in the form
// "<Code>:<Message>". Fields that are unset use [unknown] as a
// placeholder.
func (r Reason) String() string {
	code := unknown
	if r.Code != 0 {
		code = fmt.Sprintf("%d", r.Code)
	}

	message := unknown
	if r.Message != "" {
		message = r.Message
	}

	return code + ":" + message
}

// SnapdUser represents the identity of a user for security log events.
type SnapdUser struct {
	ID             int64     `json:"snapd_user_id"`
	StoreUserName  string    `json:"store_user_name"`
	StoreUserEmail string    `json:"store_user_email"`
	Expiration     time.Time `json:"expiration"`
}

// Peer describes the Unix-domain peer of an API request (Socket, UID, PID).
//
// Callers may signal "unknown" by setting UID to [peerNobody] and/or PID to
// [peerNoProcess]. These mirror the daemon ucrednetNobody and ucrednetNoProcess
// sentinels (see daemon/ucrednet.go).
type Peer struct {
	Socket string `json:"socket"`
	UID    uint32 `json:"uid"`
	PID    int32  `json:"pid"`
}

// [peerNobody] and [peerNoProcess] mirror the daemon ucrednetNobody and
// ucrednetNoProcess sentinels. They are duplicated here to keep seclog
// free of snapd package imports.
const (
	peerNobody    = ^uint32(0)
	peerNoProcess = int32(0)
)

// String returns a colon-separated representation in the form
// "<Socket>:<UID>:<PID>". Fields that are unset, or set to a documented
// "unknown" sentinel ([peerNobody], [peerNoProcess]), use [unknown] as a
// placeholder.
func (p Peer) String() string {
	socket := unknown
	if p.Socket != "" {
		socket = p.Socket
	}

	uid := unknown
	// 0 is a valid UID (root); only [peerNobody] is unknown.
	if p.UID != peerNobody {
		uid = fmt.Sprintf("%d", p.UID)
	}

	pid := unknown
	if p.PID != peerNoProcess {
		pid = fmt.Sprintf("%d", p.PID)
	}

	return socket + ":" + uid + ":" + pid
}

// Endpoint describes an API endpoint involved in an authorization event.
type Endpoint struct {
	Method        string `json:"method"`
	Path          string `json:"path"`
	Action        string `json:"action"`
	AccessChecker string `json:"access_checker"`
	AccessLevel   string `json:"access_level"`
}

// String returns a colon-separated representation in the form
// "<Method>:<Path>:<Action>". Unset method and path use [unknown]; an empty
// action is rendered as "<none>".
func (e Endpoint) String() string {
	method := unknown
	if e.Method != "" {
		method = e.Method
	}

	path := unknown
	if e.Path != "" {
		path = e.Path
	}

	action := none
	if e.Action != "" {
		action = e.Action
	}

	return method + ":" + path + ":" + action
}

// AuthzCheck represents the outcome of a single authorization check.
type AuthzCheck string

// AuthzCheck outcome values for a single authorization stage.
// The default for a new [AuthzChecks] is [AuthzNotApplicable].
const (
	AuthzNotApplicable AuthzCheck = "not-applicable" // stage not used for this request
	AuthzNotReached    AuthzCheck = "not-reached"    // applicable but not evaluated yet
	AuthzFail          AuthzCheck = "fail"
	AuthzPass          AuthzCheck = "pass"
)

// AuthzChecks captures the outcome of each authorization stage evaluated
// during an access check. Each field records whether that stage passed,
// failed, or was not applicable to the request.
type AuthzChecks struct {
	AccessOptions AuthzCheck `json:"access_options"`
	PeerCreds     AuthzCheck `json:"peer_credentials"`
	Socket        AuthzCheck `json:"socket"`
	Interface     AuthzCheck `json:"interface_requirements"`
	OpenAccess    AuthzCheck `json:"open_access"`
	UserAuth      AuthzCheck `json:"user_authentication"`
	Root          AuthzCheck `json:"root"`
	Polkit        AuthzCheck `json:"polkit"`
}

// NewAuthzChecks returns an [AuthzChecks] with all fields set to [AuthzNotApplicable].
func NewAuthzChecks() AuthzChecks {
	return AuthzChecks{
		AccessOptions: AuthzNotApplicable,
		PeerCreds:     AuthzNotApplicable,
		Socket:        AuthzNotApplicable,
		Interface:     AuthzNotApplicable,
		OpenAccess:    AuthzNotApplicable,
		UserAuth:      AuthzNotApplicable,
		Root:          AuthzNotApplicable,
		Polkit:        AuthzNotApplicable,
	}
}

// String returns a colon-separated description of the user in the form
// "<ID>:<StoreUserEmail>:<StoreUserName>". Fields that are unset use
// [unknown] as a placeholder; a zero ID is considered unset.
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

// SystemUserAddReason identifies why a system user account was created.
// Values are logged as add_reason on user_created_system events.
type SystemUserAddReason string

// SystemUserAddReason values for user_created_system events.
const (
	// AddReasonAdminStore is set when an operator requested a user from store email lookup.
	AddReasonAdminStore SystemUserAddReason = "admin-store"
	// AddReasonAdminAssertion is set when an operator requested one assertion-backed user by email.
	AddReasonAdminAssertion SystemUserAddReason = "admin-assertion"
	// AddReasonAdminKnownAll is set when an operator requested all valid assertion users.
	AddReasonAdminKnownAll SystemUserAddReason = "admin-known-all"
	// AddReasonAutoProvision is set for unattended automatic provisioning via the
	// user-admin API (automatic: true), e.g. snap auto-import.
	AddReasonAutoProvision SystemUserAddReason = "admin-auto-provision"
	// AddReasonSeedFirstboot is set when users are created from seed auto-import on dangerous models.
	AddReasonSeedFirstboot SystemUserAddReason = "seed-firstboot"
	// AddReasonSerialBound is set when a serial-bound assertion is applied after registration.
	AddReasonSerialBound SystemUserAddReason = "serial-bound"
)

// SystemUserRemoveReason identifies why a system user account was removed.
// Values are logged as remove_reason on user_removed_system events.
type SystemUserRemoveReason string

// SystemUserRemoveReason values for user_removed_system events.
const (
	// RemoveReasonAdminRemove is set when an operator explicitly removed the account.
	RemoveReasonAdminRemove SystemUserRemoveReason = "admin-remove"
	// RemoveReasonExpired is set when an expired account was removed by the ensure loop.
	RemoveReasonExpired SystemUserRemoveReason = "expired"
)

// Ref identifies an assertion by type and primary key. It mirrors asserts.Ref
// but uses plain strings so seclog stays import-free.
type Ref struct {
	Type       string   `json:"type"`
	PrimaryKey []string `json:"primary_key"`
	// Revision is the assertion revision applied when the user was created.
	// It supplements the ref; the ref itself is the store-shared identity.
	Revision int `json:"revision,omitempty"`
}

// AddOptions holds the options recorded for a system user creation event.
// JSON tags match the security audit specification field names.
type AddOptions struct {
	// RealUserName is the display name from the GECOS field of the created
	// account. devicestate populates Gecos as "email,display-name" for
	// osutil.AddUser; this field is the portion after the comma.
	RealUserName string `json:"real_user_name"`
	// Sudoer is true when the account was created with sudo privileges.
	Sudoer bool `json:"sudoer"`
	// ExtraUsers is true when the account was created in the extrausers
	// database (Ubuntu Core) rather than /etc/passwd.
	ExtraUsers bool `json:"extra_users"`
	// ForcePasswordChange is true when the user must change their password
	// on first login.
	ForcePasswordChange bool `json:"force_password_change"`
	// Known is true when the account was created from a system-user assertion
	// rather than from a store email lookup.
	Known bool `json:"known"`
	// Assertion is set when Known is true; identifies the system-user assertion used.
	Assertion *Ref `json:"assertion,omitempty"`
}

// RemoveOptions holds the options recorded for a system user removal event.
// JSON tags match the security audit specification field names.
type RemoveOptions struct {
	// Force is true when the account was removed even if it was logged in.
	Force bool `json:"force"`
}
