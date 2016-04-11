// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snapstate

import (
	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/overlord/state"
)

type SnapStateForTests snapState

type ManagerBackend managerBackend

func SetSnapManagerBackend(s *SnapManager, b ManagerBackend) {
	s.backend = b
}

func SetSnapstateBackend(b ManagerBackend) {
	backend = b
}

// AddForeignTaskHandlers registers handlers for tasks handled outside of the snap manager.
func (m *SnapManager) AddForeignTaskHandlers() {
	// Add fake handlers for tasks handled by interfaces manager
	fakeHandler := func(task *state.Task, _ *tomb.Tomb) error { return nil }
	m.runner.AddHandler("setup-snap-security", fakeHandler, fakeHandler)
	m.runner.AddHandler("remove-snap-security", fakeHandler, fakeHandler)
}
