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

// Package ifacestate implements the manager and state aspects
// responsible for the maintenance of interfaces the system.
package ifacestate

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/apparmor"
	"github.com/ubuntu-core/snappy/interfaces/builtin"
	"github.com/ubuntu-core/snappy/interfaces/dbus"
	"github.com/ubuntu-core/snappy/interfaces/seccomp"
	"github.com/ubuntu-core/snappy/interfaces/udev"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
)

// InterfaceManager is responsible for the maintenance of interfaces in
// the system state.  It maintains interface connections, and also observes
// installed snaps to track the current set of available plugs and slots.
type InterfaceManager struct {
	state  *state.State
	runner *state.TaskRunner
	repo   *interfaces.Repository
}

// Manager returns a new InterfaceManager.
func Manager(s *state.State) (*InterfaceManager, error) {
	repo := interfaces.NewRepository()
	for _, iface := range builtin.Interfaces() {
		if err := repo.AddInterface(iface); err != nil {
			return nil, err
		}
	}
	runner := state.NewTaskRunner(s)
	m := &InterfaceManager{
		state:  s,
		runner: runner,
		repo:   repo,
	}

	runner.AddHandler("connect", m.doConnect)
	runner.AddHandler("disconnect", m.doDisconnect)
	runner.AddHandler("ensure-security", m.doEnsureSecurity)
	return m, nil
}

// Connect returns a set of tasks for connecting an interface.
func Connect(s *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	// TODO: Store the intent-to-connect in the state so that we automatically
	// try to reconnect on reboot (reconnection can fail or can connect with
	// different parameters so we cannot store the actual connection details).
	summary := fmt.Sprintf(i18n.G("Connect %s:%s to %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	task := s.NewTask("connect", summary)
	task.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	task.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	return state.NewTaskSet(task), nil
}

// Disconnect returns a set of tasks for  disconnecting an interface.
func Disconnect(s *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	// TODO: Remove the intent-to-connect from the state so that we no longer
	// automatically try to reconnect on reboot.
	summary := fmt.Sprintf(i18n.G("Disconnect %s:%s from %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	task := s.NewTask("disconnect", summary)
	task.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	task.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	return state.NewTaskSet(task), nil
}

// EnsureSecurity ensures that security for a given snaps is correct.
//
// This method can be safely called with snaps that have been removed.
func EnsureSecurity(s *state.State, snapNames []string) (*state.TaskSet, error) {
	summary := fmt.Sprintf(i18n.G("Update security for snaps: %s"), strings.Join(snapNames, ", "))
	task := s.NewTask("ensure-security", summary)
	task.Set("snaps", snapNames)
	return state.NewTaskSet(task), nil
}

func getPlugAndSlotRefs(task *state.Task) (*interfaces.PlugRef, *interfaces.SlotRef, error) {
	var plugRef interfaces.PlugRef
	var slotRef interfaces.SlotRef
	if err := task.Get("plug", &plugRef); err != nil {
		return nil, nil, err
	}
	if err := task.Get("slot", &slotRef); err != nil {
		return nil, nil, err
	}
	return &plugRef, &slotRef, nil
}

func (m *InterfaceManager) doConnect(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}
	return m.repo.Connect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
}

func (m *InterfaceManager) doDisconnect(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}
	return m.repo.Disconnect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
}

func (m *InterfaceManager) doEnsureSecurity(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	var snapNames []string
	if err := task.Get("snaps", &snapNames); err != nil {
		return err
	}
	cfgs := []interfaces.SecurityConfigurator{
		&apparmor.Configurator{}, &seccomp.Configurator{},
		&dbus.Configurator{}, &udev.Configurator{},
	}
	// NOTE: This ensures that if we fail on anything mid-way some important
	// bookkeeping happens regardless of the failure. In practice this runs
	// some post-processing commands.
	//
	// FIXME: errors return from Finalize are lost
	for _, cfg := range cfgs {
		defer cfg.Finalize()
	}
	// Apply the security, snap-by-snap
	for _, snapName := range snapNames {
		// XXX: This should be something we can get from another package.
		snapYamlFilename := fmt.Sprintf("%s/%s/current/meta/snap.yaml", dirs.SnapSnapsDir, snapName)
		yamlData, err := ioutil.ReadFile(snapYamlFilename)
		switch {
		case err == nil:
			snapInfo, err := snap.InfoFromSnapYaml(yamlData)
			if err != nil {
				return err
			}
			// TODO: Information about the developer mode should be stored in the state.
			developerMode := false
			for _, cfg := range cfgs {
				if err := cfg.ConfigureSnapSecurity(m.repo, snapInfo, developerMode); err != nil {
					return err
				}
			}
		case os.IsNotExist(err):
			// Create just enough of snap.Info to remove leftovers of the snap
			// from the filesystem. We jut need the name of the snap here.
			snapInfo := &snap.Info{Name: snapName}
			for _, cfg := range cfgs {
				if err := cfg.DeconfigureSnapSecurity(snapInfo); err != nil {
					return err
				}
			}
		case err != nil:
			return err
		}
	}
	return nil
}

// Ensure implements StateManager.Ensure.
func (m *InterfaceManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *InterfaceManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *InterfaceManager) Stop() {
	m.runner.Stop()

}
