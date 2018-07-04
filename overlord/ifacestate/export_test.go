/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package ifacestate

import (
	"time"

	"github.com/snapcore/snapd/overlord/state"
)

var (
	AddImplicitSlots          = addImplicitSlots
	SnapsWithSecurityProfiles = snapsWithSecurityProfiles
	CheckConnectConflicts     = checkConnectConflicts
	FindSymmetricAutoconnect  = findSymmetricAutoconnect
	ConnectPriv               = connect
)

func MockRemoveStaleConnections(f func(st *state.State) error) (restore func()) {
	old := removeStaleConnections
	removeStaleConnections = f
	return func() { removeStaleConnections = old }
}

func MockContentLinkRetryTimeout(d time.Duration) (restore func()) {
	old := contentLinkRetryTimeout
	contentLinkRetryTimeout = d
	return func() { contentLinkRetryTimeout = old }
}
