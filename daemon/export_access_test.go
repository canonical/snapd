// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
)

type (
	AccessChecker = accessChecker

	OpenAccess          = openAccess
	AuthenticatedAccess = authenticatedAccess
	RootAccess          = rootAccess
	SnapAccess          = snapAccess
)

var CheckPolkitActionImpl = checkPolkitActionImpl

func MockCheckPolkitAction(new func(r *http.Request, ucred *Ucrednet, action string) Response) (restore func()) {
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
