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

package repo

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/backends"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

func New(st *state.State, activeSnaps []*snap.Info, extraInterfaces []interfaces.Interface, extraBackends []interfaces.SecurityBackend) (*interfaces.Repository, error) {
	repo := interfaces.NewRepository()
	if err := addInterfaces(repo, extraInterfaces); err != nil {
		return nil, err
	}
	if err := addBackends(repo, extraBackends); err != nil {
		return nil, err
	}
	if err := addSnaps(activeSnaps, repo); err != nil {
		return nil, err
	}
	if err := renameCorePlugConnection(st); err != nil {
		return nil, err
	}
	if err := ReloadConnections(st, repo, ""); err != nil {
		return nil, err
	}

	return repo, nil
}

func addInterfaces(repo *interfaces.Repository, extra []interfaces.Interface) error {
	for _, iface := range builtin.Interfaces() {
		if err := repo.AddInterface(iface); err != nil {
			return err
		}
	}
	for _, iface := range extra {
		if err := repo.AddInterface(iface); err != nil {
			return err
		}
	}
	return nil
}

func addBackends(repo *interfaces.Repository, extra []interfaces.SecurityBackend) error {
	for _, backend := range backends.All {
		if err := backend.Initialize(); err != nil {
			return err
		}
		if err := repo.AddBackend(backend); err != nil {
			return err
		}
	}
	for _, backend := range extra {
		if err := backend.Initialize(); err != nil {
			return err
		}
		if err := repo.AddBackend(backend); err != nil {
			return err
		}
	}
	return nil
}

// addImplicitSlots adds implicitly defined slots to a given snap.
//
// Only the OS snap has implicit slots.
//
// It is assumed that slots have names matching the interface name. Existing
// slots are not changed, only missing slots are added.
func AddImplicitSlots(snapInfo *snap.Info) {
	if snapInfo.Type != snap.TypeOS {
		return
	}
	// Ask each interface if it wants to be implcitly added.
	for _, iface := range builtin.Interfaces() {
		si := interfaces.StaticInfoOf(iface)
		if snapInfo.Slots == nil {
			snapInfo.Slots = make(map[string]*snap.SlotInfo)
		}
		if (release.OnClassic && si.ImplicitOnClassic) || (!release.OnClassic && si.ImplicitOnCore) {
			ifaceName := iface.Name()
			if _, ok := snapInfo.Slots[ifaceName]; !ok {
				snapInfo.Slots[ifaceName] = makeImplicitSlot(snapInfo, ifaceName)
			}
		}
	}
}

func addSnaps(snaps []*snap.Info, repo *interfaces.Repository) error {
	for _, snapInfo := range snaps {
		AddImplicitSlots(snapInfo)
		if err := repo.AddSnap(snapInfo); err != nil {
			logger.Noticef("%s", err)
		}
	}
	return nil
}

// reloadConnections reloads connections stored in the state in the repository.
// Using non-empty snapName the operation can be scoped to connections
// affecting a given snap.
func ReloadConnections(st *state.State, repo *interfaces.Repository, snapName string) error {
	conns, err := GetConns(st)
	if err != nil {
		return err
	}
	for id := range conns {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return err
		}
		if snapName != "" && connRef.PlugRef.Snap != snapName && connRef.SlotRef.Snap != snapName {
			continue
		}
		if err := repo.Connect(connRef); err != nil {
			logger.Noticef("%s", err)
		}
	}
	return nil
}

// renameCorePlugConnection renames one connection from "core-support" plug to
// slot so that the plug name is "core-support-plug" while the slot is
// unchanged. This matches a change introduced in 2.24, where the core snap no
// longer has the "core-support" plug as that was clashing with the slot with
// the same name.
func renameCorePlugConnection(st *state.State) error {
	conns, err := GetConns(st)
	if err != nil {
		return err
	}
	const oldPlugName = "core-support"
	const newPlugName = "core-support-plug"
	// old connection, note that slotRef is the same in both
	slotRef := interfaces.SlotRef{Snap: "core", Name: oldPlugName}
	oldPlugRef := interfaces.PlugRef{Snap: "core", Name: oldPlugName}
	oldConnRef := interfaces.ConnRef{PlugRef: oldPlugRef, SlotRef: slotRef}
	oldID := oldConnRef.ID()
	// if the old connection is saved, replace it with the new connection
	if cState, ok := conns[oldID]; ok {
		newPlugRef := interfaces.PlugRef{Snap: "core", Name: newPlugName}
		newConnRef := interfaces.ConnRef{PlugRef: newPlugRef, SlotRef: slotRef}
		newID := newConnRef.ID()
		delete(conns, oldID)
		conns[newID] = cState
		SetConns(st, conns)
	}
	return nil
}

func makeImplicitSlot(snapInfo *snap.Info, ifaceName string) *snap.SlotInfo {
	return &snap.SlotInfo{
		Name:      ifaceName,
		Snap:      snapInfo,
		Interface: ifaceName,
	}
}

type ConnState struct {
	Auto      bool   `json:"auto,omitempty"`
	Interface string `json:"interface,omitempty"`
}

func GetConns(st *state.State) (map[string]ConnState, error) {
	// Get information about connections from the state
	var conns map[string]ConnState
	err := st.Get("conns", &conns)
	if err != nil && err != state.ErrNoState {
		return nil, fmt.Errorf("cannot obtain data about existing connections: %s", err)
	}
	if conns == nil {
		conns = make(map[string]ConnState)
	}
	return conns, nil
}

func SetConns(st *state.State, conns map[string]ConnState) {
	st.Set("conns", conns)
}
