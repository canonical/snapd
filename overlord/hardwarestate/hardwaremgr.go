// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package hardwarestate

import (
	"sync"

	"github.com/snapcore/snapd/overlord/state"
)

type HardwareManager struct {
	state   *state.State
	udevMon udevMonitor
	once    sync.Once
}

// Manager returns a new HardwareManager.
// Extra interfaces can be provided for testing.
func Manager(s *state.State) (*HardwareManager, error) {
	m := &HardwareManager{
		udevMon: NewUDevMonitor(),
		state:   s,
	}
	return m, nil
}

func (m *HardwareManager) KnownTaskKinds() []string {
	return nil
}

// Ensure implements StateManager.Ensure.
func (m *HardwareManager) Ensure() (err error) {
	// udevMon.Run starts own goroutine
	m.once.Do(func() {
		err = m.udevMon.Run()
	})
	return err
}

// Wait implements StateManager.Wait.
func (m *HardwareManager) Wait() {
}

// Stop implements StateManager.Stop.
func (m *HardwareManager) Stop() {
	m.udevMon.Stop()
}
