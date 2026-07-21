// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2024 Canonical Ltd
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
	"net/http"

	"github.com/snapcore/snapd/polkit"
	"github.com/snapcore/snapd/seclog"
)

type (
	AccessChecker = accessChecker

	AccessOptions = accessOptions

	OpenAccess                   = openAccess
	AuthenticatedAccess          = authenticatedAccess
	RootAccess                   = rootAccess
	SnapAccess                   = snapAccess
	InterfaceOpenAccess          = interfaceOpenAccess
	InterfaceAuthenticatedAccess = interfaceAuthenticatedAccess
	InterfaceProviderRootAccess  = interfaceProviderRootAccess
	InterfaceRootAccess          = interfaceRootAccess
	ByActionAccess               = byActionAccess

	InterfaceAccessReqs = interfaceAccessReqs

	InterfaceAccessOutcome = interfaceAccessOutcome

	AccessLevel = accessLevel
)

const (
	AccessLevelOpen          = accessLevelOpen
	AccessLevelAuthenticated = accessLevelAuthenticated
	AccessLevelRoot          = accessLevelRoot
)

var (
	CheckAccess                   = checkAccess
	CheckPolkitActionImpl         = checkPolkitActionImpl
	RequireInterfaceApiAccessImpl = requireInterfaceApiAccessImpl
)

func MockCheckPolkitAction(new func(r *http.Request, ucred *Ucrednet, action string) *APIError) (restore func()) {
	old := checkPolkitAction
	checkPolkitAction = new
	return func() {
		checkPolkitAction = old
	}
}

func MockPolkitCheckAuthorization(new func(pid int32, uid uint32, actionId string, details map[string]string, flags polkit.CheckFlags) (bool, error)) (restore func()) {
	old := polkitCheckAuthorization
	polkitCheckAuthorization = new
	return func() {
		polkitCheckAuthorization = old
	}
}

func MockCgroupSnapNameFromPid(new func(pid int) (string, error)) (restore func()) {
	old := cgroupSnapNameFromPid
	cgroupSnapNameFromPid = new
	return func() {
		cgroupSnapNameFromPid = old
	}
}

func MockRequireInterfaceApiAccess(new func(d *Daemon, r *http.Request, ucred *ucrednet, reqs InterfaceAccessReqs, rec AuthzRecorder, level AccessLevel) (InterfaceAccessOutcome, *apiError)) (restore func()) {
	old := requireInterfaceApiAccess
	requireInterfaceApiAccess = new
	return func() {
		requireInterfaceApiAccess = old
	}
}

// AuthzTestRecorder records the last Record* outcome for tests.
type AuthzTestRecorder struct {
	GrantedReason string
	GrantedIface  string
	GrantedPlug   bool
	DeniedReason  string
}

func NewAuthzTestRecorder() *AuthzTestRecorder {
	return &AuthzTestRecorder{}
}

func (rec *AuthzTestRecorder) WithUser(seclog.SnapdUser) AuthzRecorder {
	return rec
}

func (rec *AuthzTestRecorder) WithPeer(seclog.Peer) AuthzRecorder {
	return rec
}

func (rec *AuthzTestRecorder) WithEndpoint(seclog.Endpoint) AuthzRecorder {
	return rec
}

func (rec *AuthzTestRecorder) RecordGranted(reason, iface string, plug bool) {
	rec.DeniedReason = ""
	rec.GrantedReason = reasonGrantedWithInterface(reason, iface, plug)
	rec.GrantedIface = iface
	rec.GrantedPlug = plug
}

func (rec *AuthzTestRecorder) RecordDenied(reason string) {
	rec.GrantedReason = ""
	rec.GrantedIface = ""
	rec.GrantedPlug = false
	rec.DeniedReason = reason
}

func (rec *AuthzTestRecorder) Emit() {}

var _ AuthzRecorder = (*AuthzTestRecorder)(nil)
