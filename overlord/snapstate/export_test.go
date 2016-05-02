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
	"errors"

	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
)

type ManagerBackend managerBackend

func SetSnapManagerBackend(s *SnapManager, b ManagerBackend) {
	s.backend = b
}

func SetSnapstateBackend(b ManagerBackend) {
	backend = b
}

type ForeignTaskTracker interface {
	ForeignTask(kind string, status state.Status, ss *SnapSetup)
}

// AddForeignTaskHandlers registers handlers for tasks handled outside of the snap manager.
func (m *SnapManager) AddForeignTaskHandlers(tracker ForeignTaskTracker) {
	// Add fake handlers for tasks handled by interfaces manager
	fakeHandler := func(task *state.Task, _ *tomb.Tomb) error {
		task.State().Lock()
		kind := task.Kind()
		status := task.Status()
		ss, err := TaskSnapSetup(task)
		task.State().Unlock()
		if err != nil {
			return err
		}

		tracker.ForeignTask(kind, status, ss)

		return nil
	}
	m.runner.AddHandler("setup-profiles", fakeHandler, fakeHandler)
	m.runner.AddHandler("remove-profiles", fakeHandler, fakeHandler)
	m.runner.AddHandler("discard-conns", fakeHandler, fakeHandler)

	// Add handler to test full aborting of changes
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	m.runner.AddHandler("error-trigger", erroringHandler, nil)
}

func MockReadInfo(mock func(name string, si *snap.SideInfo) (*snap.Info, error)) func() {
	readInfo = mock
	return func() { readInfo = snap.ReadInfo }
}
