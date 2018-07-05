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
	"errors"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var (
	SnapsWithSecurityProfiles = snapsWithSecurityProfiles
	CheckConnectConflicts     = checkConnectConflicts
	FindSymmetricAutoconnect  = findSymmetricAutoconnect
	HasSnapdSnap              = hasSnapdSnap
)

func (m *InterfaceManager) ConnectPriv(plugSnap, plugName, slotSnap, slotName string, flags []string) (*state.TaskSet, error) {
	return m.connect(plugSnap, plugName, slotSnap, slotName, flags)
}

func (m *InterfaceManager) AddImplicitSlots(snapInfo *snap.Info) {
	m.addImplicitSlots(snapInfo)
}

// AddForeignTaskHandlers registers handlers for tasks handled outside of the
// InterfaceManager.
func AddForeignTaskHandlers(m *InterfaceManager) {
	// Add handler to test full aborting of changes
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	m.runner.AddHandler("error-trigger", erroringHandler, nil)
}

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
