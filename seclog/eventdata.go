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

// LSM security label keys for [Peer.SecurityLabels].
const (
	PeerSecurityLabelAppArmor = "AppArmor"
	PeerSecurityLabelSELinux  = "SELinux"
)

// Peer describes the Unix-domain peer of an API request.
//
// Socket, UID, and PID come from peer credentials and are expected to be
// set when emitting AUTHZ events (the access gate is not reached without
// them). Exe, CgroupLabel, Snap, and App are best-effort enrichment fields.
// When unavailable, leave them empty or set them to [unknown]; [Peer.LogValue]
// logs empty values as [unknown].
//
// [Peer.SecurityLabels] is also best-effort enrichment: include only the LSM
// keys that were obtained. Do not use [unknown] as a map value; omit unavailable
// keys instead. An empty or nil map is logged as an empty JSON object. Keys are
// emitted in alphabetical order.
//
// Callers may signal "unknown" by setting UID to [peerNobody] and/or PID to
// [peerNoProcess] for display via [Peer.String]; these mirror the daemon
// `ucrednetNobody` and `ucrednetNoProcess` sentinels (see daemon/ucrednet.go).
type Peer struct {
	Socket string `json:"socket"`
	UID    uint32 `json:"uid"`
	PID    int32  `json:"pid"`
	// Exe is the executable path of the peer process, read from
	// /proc/<pid>/exe. [unknown] when unavailable.
	Exe string `json:"exe"`
	// SecurityLabels holds LSM security labels keyed by [PeerSecurityLabelAppArmor]
	// and [PeerSecurityLabelSELinux]. Omit unavailable keys.
	SecurityLabels map[string]string `json:"security_labels"`
	// CgroupLabel is the snap cgroup label of the peer process (e.g.
	// snap.<instance>.<app>). [unknown] when unavailable.
	CgroupLabel string `json:"cgroup_label"`
	// Snap is the snap instance name of the peer process, typically derived
	// from the AppArmor entry in [Peer.SecurityLabels]. [unknown] when unavailable.
	Snap string `json:"snap"`
	// App is the snap application or service name of the peer process,
	// typically derived from the AppArmor entry in [Peer.SecurityLabels].
	// [unknown] when unavailable.
	App string `json:"app"`
}

// [peerNobody] and [peerNoProcess] mirror the daemon `ucrednetNobody` and
// `ucrednetNoProcess` sentinels. They are duplicated here to keep seclog
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
// Values follow {trigger}-create-user-from-{source} (with optional modifiers)
// and describe where account details came from, not who invoked the operation.
// They are logged as add_reason on user_created_system events.
type SystemUserAddReason string

// SystemUserAddReason values for user_created_system events.
const (
	// AddReasonAPIStoreEmail is set when POST /v2/users or POST /v2/create-user
	// creates a system user with known: false; account details are looked up
	// from the Snap Store by email.
	AddReasonAPIStoreEmail SystemUserAddReason = "api-store-email"
	// AddReasonAPIAssertion is set when POST /v2/users or POST /v2/create-user
	// creates one user with known: true and an email; details come from a
	// pre-imported system-user assertion selected by brand-id and email.
	AddReasonAPIAssertion SystemUserAddReason = "api-assertion"
	// AddReasonAPIAssertionAll is set when POST /v2/users or POST /v2/create-user
	// creates every applicable system user with known: true and no email; details
	// come from all valid pre-imported system-user assertions for the device
	// model. This is the explicit admin path (e.g. snap create-user --known):
	// sudoer is optional, and the request fails if the device is already managed.
	AddReasonAPIAssertionAll SystemUserAddReason = "api-assertion-all"
	// AddReasonAPIAssertionAllAutomatic is set when POST /v2/users
	// or POST /v2/create-user is called with automatic: true. Like
	// [AddReasonAPIAssertionAll], all applicable assertion-backed users are
	// created, but this is the automation path: callers are typically
	// background machinery (e.g. snap auto-import after a USB hardware event)
	// rather than an operator. The API forces sudoer, respects
	// core.users.create.automatic, and treats an already-managed device as a
	// silent no-op instead of an error so repeated or late triggers do not
	// fail noisily when provisioning has already succeeded.
	AddReasonAPIAssertionAllAutomatic SystemUserAddReason = "api-assertion-all-automatic"
	// AddReasonFirstbootSeedAutoImport is set during first boot on dangerous
	// models when system-user assertions from the seed are auto-imported and
	// applied (not via the user-admin API).
	AddReasonFirstbootSeedAutoImport SystemUserAddReason = "firstboot-seed-auto-import"
	// AddReasonEnsureSerialBoundAssertion is set when the device manager
	// ensure loop creates users from serial-bound system-user assertions that
	// could not be applied until after device registration.
	AddReasonEnsureSerialBoundAssertion SystemUserAddReason = "ensure-serial-bound-assertion"
)

// SystemUserRemoveReason identifies why a system user account was removed.
// Values follow {trigger}-remove-{cause} and describe the trigger, not who
// invoked the operation. They are logged as remove_reason on
// user_removed_system events.
type SystemUserRemoveReason string

// SystemUserRemoveReason values for user_removed_system events.
const (
	// RemoveReasonAPIRemoveUser is set when POST /v2/users (DELETE) or
	// POST /v2/create-user remove-user removes the account explicitly.
	RemoveReasonAPIRemoveUser SystemUserRemoveReason = "api-remove-user"
	// RemoveReasonEnsureRemoveExpiredUser is set when the device manager ensure
	// loop removes an account whose assertion or API-provided expiration time
	// has passed.
	RemoveReasonEnsureRemoveExpiredUser SystemUserRemoveReason = "ensure-remove-expired-user"
)

// Ref identifies an assertion by primary key. It mirrors the primary-key
// portion of asserts.Ref but uses plain strings so seclog stays import-free.
type Ref struct {
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
