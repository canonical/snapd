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
//  2. Self-contained event types: seclog is imported by packages such as
//     overlord/auth, so it cannot import them back. Event types here must
//     not embed those packages' types; callers in such packages still
//     translate (e.g. [auth.UserState] → [SnapdUser]). Conversion helpers
//     may import utility packages (osutil, asserts) that will never need
//     to log.
//
// When adding a new event category, define its types here.

package seclog

import (
	"fmt"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/osutil"
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

// Peer describes the Unix-domain peer of an API request.
//
// Socket, UID, and PID come from peer credentials and are expected to be
// set when emitting AUTHZ events (the access gate is not reached without
// them). Exe, Snap, and Runnable are best-effort enrichment fields.
// When unavailable, leave them empty; [Peer.LogValue] logs empty values as
// [unknown].
//
// Callers may signal "unknown" by setting UID to [PeerNobody] and/or PID to
// [PeerNoProcess] for display via [Peer.String].
type Peer struct {
	Socket string `json:"socket"`
	UID    uint32 `json:"uid"`
	PID    int32  `json:"pid"`
	// Exe is the executable path of the peer process, from /proc/<pid>/exe.
	// [unknown] when unavailable.
	Exe string `json:"exe"`
	// Snap is the snap instance name of the peer process, from the AppArmor
	// tag when that is present and not unconfined, otherwise from the cgroup
	// security tag. [unknown] when unavailable.
	Snap string `json:"snap"`
	// Runnable is the peer app or hook. Apps use an "app." log prefix plus
	// snap.Runnable.CommandName; hooks use CommandName as-is (hook.<name> or
	// <snap>+<component>.hook.<name>, snap name without instance key).
	// From the same tag as [Peer.Snap]. [unknown] when unavailable.
	Runnable string `json:"runnable"`
}

// PeerNobody and PeerNoProcess are the unknown UID and PID sentinels for [Peer].
const (
	PeerNobody    = ^uint32(0)
	PeerNoProcess = int32(0)
)

// String returns a colon-separated representation in the form
// "<Socket>:<UID>:<PID>". Fields that are unset, or set to a documented
// "unknown" sentinel ([PeerNobody], [PeerNoProcess]), use [unknown] as a
// placeholder.
func (p Peer) String() string {
	socket := unknown
	if p.Socket != "" {
		socket = p.Socket
	}

	uid := unknown
	// 0 is a valid UID (root); only [PeerNobody] is unknown.
	if p.UID != PeerNobody {
		uid = fmt.Sprintf("%d", p.UID)
	}

	pid := unknown
	if p.PID != PeerNoProcess {
		pid = fmt.Sprintf("%d", p.PID)
	}

	return socket + ":" + uid + ":" + pid
}

// Endpoint describes an API endpoint involved in an authorization event.
// When unavailable, leave Method and Path empty or set them to [unknown], and
// leave Action empty or set it to [none]; [Endpoint.LogValue] logs empty
// method and path as [unknown] and an empty action as [none].
type Endpoint struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Action string `json:"action"`
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

// GrantReason identifies why access was granted for authz_admin events.
// It is passed to [LogAdminActivity] as grantReason and emitted as
// reason_granted.
//
// The base values are [GrantUserAuth], [GrantRootAuth], and
// [GrantPolkitAuth]. When an interface connection also contributed to
// the grant, use [GrantReason.WithInterface].
type GrantReason string

const (
	GrantUserAuth   GrantReason = "user-auth"
	GrantRootAuth   GrantReason = "root-auth"
	GrantPolkitAuth GrantReason = "polkit-auth"
)

// WithInterface returns a [GrantReason] that includes a snap interface
// connection as part of why access was granted.
//
// The result has the form "<reason> <interface> <plug|slot>", for
// example "root-auth desktop-launch plug".
//
// If iface is empty, WithInterface returns g unchanged so it can be
// called unconditionally.
//
// onPlugSide is true when the requesting snap was on the plug side of
// the connection, false for the slot side.
func (g GrantReason) WithInterface(iface string, onPlugSide bool) GrantReason {
	if iface == "" {
		return g
	}
	side := "slot"
	if onPlugSide {
		side = "plug"
	}
	return GrantReason(string(g) + " " + iface + " " + side)
}

// DenialReason identifies why access was denied for authz_fail events.
// It is passed to [LogUnauthorizedAccess] as denialReason and emitted as
// reason_denied.
type DenialReason string

const (
	DenialNoPeerCredentials    DenialReason = "no-peer-credentials"
	DenialSocketNotPermitted   DenialReason = "socket-not-permitted"
	DenialMissingInterfacePlug DenialReason = "missing-interface-plug"
	DenialMissingInterfaceSlot DenialReason = "missing-interface-slot"
	DenialUserAuth             DenialReason = "user-auth-denied"
	DenialRootAuth             DenialReason = "root-auth-denied"
	DenialPolkitAuth           DenialReason = "polkit-auth-denied"
)

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
// Values follow {trigger}-{source}: the subsystem that started the operation
// and where the account details came from. They are logged as add_reason on
// user_created_system events.
type SystemUserAddReason string

// SystemUserAddReason values for user_created_system events. The api-* values
// are set by the user-admin API (POST /v2/users, POST /v2/create-user).
const (
	// AddReasonAPIStoreEmail is set when a user is created from store account
	// details looked up by email.
	AddReasonAPIStoreEmail SystemUserAddReason = "api-store-email"
	// AddReasonAPIAssertion is set when a single user is created from the
	// system-user assertion for a given email.
	AddReasonAPIAssertion SystemUserAddReason = "api-assertion"
	// AddReasonAPIAssertionAutomatic is like [AddReasonAPIAssertion], but the
	// request came from automation rather than an operator.
	AddReasonAPIAssertionAutomatic SystemUserAddReason = "api-assertion-automatic"
	// AddReasonAPIAssertionAll is set when every user allowed by the device's
	// system-user assertions is created.
	AddReasonAPIAssertionAll SystemUserAddReason = "api-assertion-all"
	// AddReasonAPIAssertionAllAutomatic is like [AddReasonAPIAssertionAll],
	// but the request came from automation rather than an operator.
	AddReasonAPIAssertionAllAutomatic SystemUserAddReason = "api-assertion-all-automatic"
	// AddReasonFirstbootSeedAutoImport is set when auto-import assertions from
	// the seed are applied during first boot, on dangerous models only.
	AddReasonFirstbootSeedAutoImport SystemUserAddReason = "firstboot-seed-auto-import"
	// AddReasonEnsureSerialBoundAssertion is set when the device manager
	// applies serial-bound system-user assertions after registration.
	AddReasonEnsureSerialBoundAssertion SystemUserAddReason = "ensure-serial-bound-assertion"
)

// SystemUserRemoveReason identifies why a system user account was removed.
// Values follow {trigger}-remove-{target}: the subsystem that started the
// operation and the kind of account removed. They are logged as remove_reason
// on user_removed_system events.
type SystemUserRemoveReason string

// SystemUserRemoveReason values for user_removed_system events. The api-*
// value is set by the user-admin API (POST /v2/users, action "remove").
const (
	// RemoveReasonAPI is set when an account is removed by explicit request.
	RemoveReasonAPI SystemUserRemoveReason = "api-remove-user"
	// RemoveReasonEnsureExpired is set when the device manager removes an
	// account whose expiration time has passed.
	RemoveReasonEnsureExpired SystemUserRemoveReason = "ensure-remove-expired-user"
)

// AssertionRef identifies an assertion by type and primary key. It mirrors
// asserts.Ref but uses plain strings so the audit payload stays self-contained.
type AssertionRef struct {
	// Type is the assertion type name, e.g. "system-user".
	Type string `json:"type"`
	// PrimaryKey holds the primary key values in the order declared by Type.
	PrimaryKey []string `json:"primary_key"`
	// Revision is the assertion revision applied when the user was created.
	// It supplements the ref; the ref itself is the store-shared identity.
	Revision int `json:"revision"`
}

// SystemUserAddOptions holds the options recorded for a system user creation
// event. JSON tags match the security audit specification field names.
type SystemUserAddOptions struct {
	// RealUserName is the display name recorded for the created account,
	// taken from the account's GECOS field. For accounts created from store
	// details (see [AddReasonAPIStoreEmail]) it holds the store account
	// identifier rather than a person's name. It may be empty when no name
	// is available.
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
	Assertion *AssertionRef `json:"assertion"`
}

// AssertionRefFrom returns an [AssertionRef] for a. If a is nil,
// AssertionRefFrom returns nil.
func AssertionRefFrom(a asserts.Assertion) *AssertionRef {
	if a == nil {
		return nil
	}
	ref := a.Ref()
	return &AssertionRef{
		Type:       ref.Type.Name,
		PrimaryKey: ref.PrimaryKey,
		Revision:   a.Revision(),
	}
}

// SystemUserAddOptionsFrom builds the audit payload for a system user
// creation from the options passed to osutil.AddUser and, when known, the
// backing system-user assertion.
func SystemUserAddOptionsFrom(opts *osutil.AddUserOptions, userAssertion *asserts.SystemUser) SystemUserAddOptions {
	// RealUserName is taken from the portion of Gecos after the first comma
	// (assertion display name or store OpenID identifier).
	addOpts := SystemUserAddOptions{
		Known:               userAssertion != nil,
		Sudoer:              opts.Sudoer,
		ExtraUsers:          opts.ExtraUsers,
		ForcePasswordChange: opts.ForcePasswordChange,
	}
	if _, realUserName, ok := strings.Cut(opts.Gecos, ","); ok {
		addOpts.RealUserName = realUserName
	}
	if userAssertion != nil {
		addOpts.Assertion = AssertionRefFrom(userAssertion)
	}
	return addOpts
}

// SystemUserRemoveOptions holds the options recorded for a system user removal
// event. JSON tags match the security audit specification field names.
type SystemUserRemoveOptions struct {
	// Force is true when the account was removed even if it was logged in.
	Force bool `json:"force"`
}
