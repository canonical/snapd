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

// This file contains daemon-side helpers for building and emitting security
// audit events via the [seclog] package. Keep seclog integration out of
// daemon.go so new event categories can add helpers here without growing the
// core daemon type.

package daemon

import (
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/lsm"
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/snap/naming"
)

var (
	securityLabelsFromPid = lsm.SecurityLabelsFromPid
	cgroupPathFromPid     = cgroup.ProcessPathInTrackingCgroup
)

// seclogPeerFromUcred builds a [seclog.Peer] for AUTHZ audit events, including
// best-effort enrichment from the peer process.
func seclogPeerFromUcred(ucred *ucrednet) seclog.Peer {
	if ucred == nil {
		return seclog.Peer{
			UID: ucrednetNobody,
			PID: ucrednetNoProcess,
		}
	}
	peer := seclog.Peer{
		Socket: ucred.Socket,
		UID:    ucred.Uid,
		PID:    ucred.Pid,
	}
	return enrichSeclogPeer(peer)
}

func enrichSeclogPeer(peer seclog.Peer) seclog.Peer {
	if peer.PID == ucrednetNoProcess {
		return peer
	}

	pid := int(peer.PID)

	exePath := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%d/exe", pid))
	if exe, err := osReadlink(exePath); err == nil {
		peer.Exe = exe
	}

	if labels, err := securityLabelsFromPid(pid); err == nil {
		peer.SecurityLabels = labels
	}

	if cgroupPath, err := cgroupPathFromPid(pid); err == nil {
		if tag := cgroup.SecurityTagFromCgroupPath(cgroupPath); tag != nil {
			peer.CgroupLabel = tag.String()
		}
	}

	snap, app := snapAppFromLabel(peer.SecurityLabels[seclog.PeerSecurityLabelAppArmor])
	if snap == "" {
		snap, app = snapAppFromLabel(peer.CgroupLabel)
	}
	peer.Snap = snap
	peer.App = app

	return peer
}

func snapAppFromLabel(label string) (snap, app string) {
	if label == "" {
		return "", ""
	}
	if tag, err := naming.ParseAppSecurityTag(label); err == nil {
		return tag.InstanceName(), tag.AppName()
	}
	if tag, err := naming.ParseHookSecurityTag(label); err == nil {
		return tag.InstanceName(), tag.HookName()
	}
	return "", ""
}

// seclogEndpointFromRequest builds a [seclog.Endpoint] for AUTHZ audit events.
// action is typically obtained via [tryExtractJSONAction].
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

	// RecordGranted records access granted. reason is one of [seclog.ReasonGranted*].
	// When a snap interface connection also contributed, pass iface and set plug
	// for the plug side or clear plug for the slot side.
	RecordGranted(reason string, iface string, plug bool)

	// RecordDenied records access denied. reason is one of [seclog.ReasonDenied*].
	RecordDenied(reason string)

	// Emit writes the accumulated event. It is a no-op when no outcome was
	// recorded.
	Emit()
}

// authzRecorder holds the seclog payload for a single authorization decision.
type authzRecorder struct {
	user          seclog.SnapdUser
	peer          seclog.Peer
	endpoint      seclog.Endpoint
	reasonGranted string
	reasonDenied  string
}

// Ensure [authzRecorder] implements [AuthzRecorder].
var _ AuthzRecorder = (*authzRecorder)(nil)

// NewAuthzRecorder returns an empty [AuthzRecorder].
func NewAuthzRecorder() AuthzRecorder {
	return &authzRecorder{}
}

// nopAuthzRecorder discards all recordings.
type nopAuthzRecorder struct{}

// Ensure [nopAuthzRecorder] implements [AuthzRecorder].
var _ AuthzRecorder = nopAuthzRecorder{}

// NewNopAuthzRecorder returns an [AuthzRecorder] that discards all recordings.
func NewNopAuthzRecorder() AuthzRecorder {
	return nopAuthzRecorder{}
}

func (nopAuthzRecorder) WithUser(seclog.SnapdUser) AuthzRecorder {
	return nopAuthzRecorder{}
}

func (nopAuthzRecorder) WithPeer(seclog.Peer) AuthzRecorder {
	return nopAuthzRecorder{}
}

func (nopAuthzRecorder) WithEndpoint(seclog.Endpoint) AuthzRecorder {
	return nopAuthzRecorder{}
}

func (nopAuthzRecorder) RecordGranted(string, string, bool) {}

func (nopAuthzRecorder) RecordDenied(string) {}

func (nopAuthzRecorder) Emit() {}

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

func (rec *authzRecorder) RecordGranted(reason, iface string, plug bool) {
	rec.reasonDenied = ""
	rec.reasonGranted = reasonGrantedWithInterface(reason, iface, plug)
}

func (rec *authzRecorder) RecordDenied(reason string) {
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

func reasonGrantedWithInterface(reason, iface string, plug bool) string {
	if iface == "" {
		return reason
	}
	side := "slot"
	if plug {
		side = "plug"
	}
	return fmt.Sprintf("%s %s+%s", reason, iface, side)
}
