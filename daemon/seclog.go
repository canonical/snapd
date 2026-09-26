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

package daemon

import (
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/snap/naming"
)

// seclogPeer builds a [seclog.Peer] for AUTHZ events from the
// credentials captured at accept.
func (un *ucrednet) seclogPeer() seclog.Peer {
	// best-effort for logging
	if un == nil {
		return seclog.Peer{
			UID: seclog.PeerNobody,
			PID: seclog.PeerNoProcess,
		}
	}
	peer := seclog.Peer{
		Socket: un.Socket,
		UID:    un.Uid,
		// PIDForPolkit is the accept-time peer pid; using it here is acceptable.
		PID: un.PIDForPolkit,
	}
	// Note that untrusted is just that we have low confidence in the name, not
	// that the process itself is not trusted explicitly.
	if exe, err := un.UntrustedProcessExeName(); err == nil {
		peer.Exe = exe
	}
	if tag, err := un.SecurityTag(); err == nil {
		peer.InstanceName = naming.InstanceName(tag.InstanceName())
		peer.Runnable = seclog.RunnableFromSecurityTag(tag)
	}
	return peer
}

// seclogEndpointFromRequest builds a [seclog.Endpoint] for AUTHZ audit events.
// action is the value cached by ServeHTTP, empty when the request has none.
func seclogEndpointFromRequest(path, method, action string) seclog.Endpoint {
	return seclog.Endpoint{
		Method: method,
		Path:   path,
		Action: action,
	}
}

// AuthzRecorder accumulates one AUTHZ audit event during an access check.
// Populate [seclog.SnapdUser], [seclog.Peer], and [seclog.Endpoint] via the
// With* methods, record the outcome via Record*, then call [AuthzRecorder.Emit].
type AuthzRecorder interface {
	WithUser(user seclog.SnapdUser) AuthzRecorder
	WithPeer(peer seclog.Peer) AuthzRecorder
	WithEndpoint(endpoint seclog.Endpoint) AuthzRecorder

	// RecordGranted records access granted. reason is one of [seclog.GrantUserAuth],
	// [seclog.GrantRootAuth], or [seclog.GrantPolkitAuth]. When a snap interface
	// connection also contributed, pass iface and set onPlugSide for the plug
	// side or leave it false for the slot side. The stored reason is
	// reason.WithInterface(iface, onPlugSide). A later record replaces any
	// earlier outcome.
	RecordGranted(reason seclog.GrantReason, iface string, onPlugSide bool)

	// RecordDenied records access denied. reason is one of the [seclog.DenialReason]
	// constants. A later record replaces any earlier outcome.
	RecordDenied(reason seclog.DenialReason)

	// Emit writes the accumulated event. It is a no-op when no outcome was
	// recorded.
	Emit()
}

// authzRecorder holds the seclog payload for a single authorization decision.
type authzRecorder struct {
	user          seclog.SnapdUser
	peer          seclog.Peer
	endpoint      seclog.Endpoint
	reasonGranted seclog.GrantReason
	reasonDenied  seclog.DenialReason
}

// Ensure [authzRecorder] implements [AuthzRecorder].
var _ AuthzRecorder = (*authzRecorder)(nil)

// NewAuthzRecorder returns an empty [AuthzRecorder].
func NewAuthzRecorder() AuthzRecorder {
	return &authzRecorder{}
}

func (rec *authzRecorder) WithUser(user seclog.SnapdUser) AuthzRecorder {
	rec.user = user
	return rec
}

func (rec *authzRecorder) WithPeer(peer seclog.Peer) AuthzRecorder {
	rec.peer = peer
	return rec
}

func (rec *authzRecorder) WithEndpoint(endpoint seclog.Endpoint) AuthzRecorder {
	rec.endpoint = endpoint
	return rec
}

func (rec *authzRecorder) RecordGranted(reason seclog.GrantReason, iface string, onPlugSide bool) {
	rec.reasonDenied = ""
	rec.reasonGranted = reason.WithInterface(iface, onPlugSide)
}

func (rec *authzRecorder) RecordDenied(reason seclog.DenialReason) {
	rec.reasonGranted = ""
	rec.reasonDenied = reason
}

func (rec *authzRecorder) Emit() {
	switch {
	case rec.reasonDenied != "":
		seclog.LogUnauthorizedAccess(rec.user, rec.peer, rec.endpoint, rec.reasonDenied)
	case rec.reasonGranted != "":
		seclog.LogAdminActivity(rec.user, rec.peer, rec.endpoint, rec.reasonGranted)
	}
}

func seclogSnapdUserFromAuth(user *auth.UserState) seclog.SnapdUser {
	if user == nil {
		return seclog.SnapdUser{}
	}
	return seclog.SnapdUser{
		ID:             int64(user.ID),
		StoreUserName:  user.Username,
		StoreUserEmail: user.Email,
		Expiration:     user.Expiration,
	}
}
