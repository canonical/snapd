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

	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/builtin"
	"github.com/ubuntu-core/snappy/overlord/state"
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

	runner.AddHandler("connect", m.doConnect, nil)
	runner.AddHandler("disconnect", m.doDisconnect, nil)
	return m, nil
}

// Connect returns a set of tasks for connecting an interface.
//
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
