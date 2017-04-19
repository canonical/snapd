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

// Package devstate implements the manager responsible for device
// identity.
package devstate

import (
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
)

// DeviceManager is responsible for the management of device identity in
// the system state. It acquires and stores credentials and verifies
// that the device's identity is valid when checked against the local
// assertion database.
type DeviceManager struct {
	state     *state.State
	runner    *state.TaskRunner
	assertMgr *assertstate.AssertManager
}

// Manager returns a new DeviceManager.
func Manager(s *state.State, assertMgr *assertstate.AssertManager) (*DeviceManager, error) {
	runner := state.NewTaskRunner(s)
	manager := &DeviceManager{
		state:     s,
		runner:    runner,
		assertMgr: assertMgr,
	}

	runner.AddHandler("set-device-identity", manager.doSetDeviceIdentity, nil)

	return manager, nil
}

// Ensure implements StateManager.Ensure.
func (m *DeviceManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *DeviceManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *DeviceManager) Stop() {
	m.runner.Stop()
}

type deviceIdentity struct {
	Brand  string `json:"brand"`
	Model  string `json:"model"`
	Serial string `json:"serial"`
}

// SetDeviceIdentity returns a set of tasks that changes the device's identity.
func SetDeviceIdentity(s *state.State, brand string, model string, serial string) (*state.TaskSet, error) {
	// TODO: Ask identity provider to give us the serial assertion
	// and its dependencies, rather than failing if it's not already
	// there.
	task := s.NewTask("set-device-identity", fmt.Sprintf(i18n.G(`Set device identity to brand "%s", model "%s", serial "%s"`), brand, model, serial))
	task.Set("device-identity", deviceIdentity{Brand: brand, Model: model, Serial: serial})
	return state.NewTaskSet(task), nil
}

func (m *DeviceManager) doSetDeviceIdentity(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	var identity deviceIdentity
	err := task.Get("device-identity", &identity)
	st.Unlock()

	if err != nil {
		return fmt.Errorf("cannot extract new device identity from task: %s", err)
	}

	assert, err := m.assertMgr.DB().Find(asserts.SerialType, map[string]string{"brand-id": identity.Brand, "model": identity.Model, "serial": identity.Serial})
	if err == asserts.ErrNotFound {
		return fmt.Errorf("no matching serial assertion")
	} else if err != nil {
		return err
	}
	err = m.assertMgr.DB().Check(assert)
	if err != nil {
		return err
	}

	st.Lock()
	dev, err := auth.Device(st)
	if err != nil {
		st.Unlock()
		return err
	}
	dev.Brand = identity.Brand
	dev.Model = identity.Model
	dev.Serial = identity.Serial
	err = auth.SetDevice(st, dev)
	st.Unlock()
	return err
}
