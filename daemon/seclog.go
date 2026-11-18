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

// authzRecorder accumulates one AUTHZ audit event during an access check.
// User, peer, and endpoint are set at construction. The access level is set
// at the start of the check and decides whether an outcome is audited.
// Record the outcome via recordGranted or recordDenied, then call log.
type authzRecorder struct {
	user          seclog.SnapdUser
	peer          seclog.Peer
	endpoint      seclog.Endpoint
	level         accessLevel
	reasonGranted seclog.GrantReason
	reasonDenied  seclog.DenialReason
}

// newAuthzRecorder returns a recorder for one authorization decision.
func newAuthzRecorder(user seclog.SnapdUser, peer seclog.Peer, endpoint seclog.Endpoint) *authzRecorder {
	return &authzRecorder{
		user:     user,
		peer:     peer,
		endpoint: endpoint,
	}
}

// recordGranted records access granted. reason is one of [seclog.GrantUserAuth],
// [seclog.GrantRootAuth], or [seclog.GrantPolkitAuth]. When a snap interface
// connection also contributed, pass iface and the side the calling snap is
// on. The stored reason is reason.WithInterface(iface, side). A later record
// replaces any earlier outcome.
func (rec *authzRecorder) recordGranted(reason seclog.GrantReason, iface string, side seclog.InterfaceSide) {
	rec.reasonDenied = ""
	rec.reasonGranted = reason.WithInterface(iface, side)
}

// recordDenied records access denied. reason is one of the [seclog.DenialReason]
// constants. A later record replaces any earlier outcome.
func (rec *authzRecorder) recordDenied(reason seclog.DenialReason) {
	rec.reasonGranted = ""
	rec.reasonDenied = reason
}

// log writes the accumulated event. It is a no-op when no outcome was
// recorded.
func (rec *authzRecorder) log() {
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
