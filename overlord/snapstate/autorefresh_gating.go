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

package snapstate

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

var gateAutoRefreshHookName = "gate-auto-refresh"

type affectedSnapInfo struct {
	Restart        bool
	Base           bool
	AffectingSnaps map[string]bool
}

func affectedByRefresh(st *state.State, updates []*snap.Info) (map[string]*affectedSnapInfo, error) {
	all, err := All(st)
	if err != nil {
		return nil, err
	}

	var bootBase string
	if !release.OnClassic {
		deviceCtx, err := DeviceCtx(st, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("cannot get device context: %v", err)
		}
		bootBaseInfo, err := BootBaseInfo(st, deviceCtx)
		if err != nil {
			return nil, fmt.Errorf("cannot get boot base info: %v", err)
		}
		bootBase = bootBaseInfo.InstanceName()
	}

	byBase := make(map[string][]string)
	for name, snapSt := range all {
		if !snapSt.Active {
			delete(all, name)
			continue
		}
		inf, err := snapSt.CurrentInfo()
		if err != nil {
			return nil, err
		}
		// optimization: do not consider snaps that don't have gate-auto-refresh hook.
		if inf.Hooks[gateAutoRefreshHookName] == nil {
			delete(all, name)
			continue
		}

		base := inf.Base
		if base == "none" {
			continue
		}
		if inf.Base == "" {
			base = "core"
		}
		byBase[base] = append(byBase[base], inf.InstanceName())
	}

	affected := make(map[string]*affectedSnapInfo)

	addAffected := func(snapName, affectedBy string, restart bool, base bool) {
		if affected[snapName] == nil {
			affected[snapName] = &affectedSnapInfo{
				AffectingSnaps: map[string]bool{},
			}
		}
		affectedInfo := affected[snapName]
		if restart {
			affectedInfo.Restart = restart
		}
		if base {
			affectedInfo.Base = base
		}
		affectedInfo.AffectingSnaps[affectedBy] = true
	}

	for _, up := range updates {
		// on core system, affected by update of boot base
		if bootBase != "" && up.InstanceName() == bootBase {
			for _, snapSt := range all {
				addAffected(snapSt.InstanceName(), up.InstanceName(), true, false)
			}
		}

		// snaps that can trigger reboot
		// XXX: gadget refresh doesn't always require reboot, refine this
		if up.Type() == snap.TypeKernel || up.Type() == snap.TypeGadget {
			for _, snapSt := range all {
				addAffected(snapSt.InstanceName(), up.InstanceName(), true, false)
			}
			continue
		}
		if up.Type() == snap.TypeBase || up.SnapName() == "core" {
			// affected by refresh of this base snap
			for _, snapName := range byBase[up.InstanceName()] {
				addAffected(snapName, up.InstanceName(), false, true)
			}
		}

		repo := ifacerepo.Get(st)

		// consider slots provided by refreshed snap, but exclude core and snapd
		// since they provide system-level slots that are generally not disrupted
		// by snap updates.
		if up.SnapType != snap.TypeSnapd && up.SnapName() != "core" {
			for _, slotInfo := range up.Slots {
				conns, err := repo.Connected(up.InstanceName(), slotInfo.Name)
				if err != nil {
					return nil, err
				}
				for _, cref := range conns {
					// affected only if it wasn't optimized out above
					if all[cref.PlugRef.Snap] != nil {
						addAffected(cref.PlugRef.Snap, up.InstanceName(), true, false)
					}
				}
			}
		}

		// consider mount backend plugs/slots;
		// for slot side only consider snapd/core because they are ignored by the
		// earlier loop around slots.
		if up.SnapType == snap.TypeSnapd || up.SnapType == snap.TypeOS {
			for _, slotInfo := range up.Slots {
				iface := repo.Interface(slotInfo.Interface)
				if iface == nil {
					return nil, fmt.Errorf("internal error: unknown interface %s", slotInfo.Interface)
				}
				if !usesMountBackend(iface) {
					continue
				}
				conns, err := repo.Connected(up.InstanceName(), slotInfo.Name)
				if err != nil {
					return nil, err
				}
				for _, cref := range conns {
					if all[cref.PlugRef.Snap] != nil {
						addAffected(cref.PlugRef.Snap, up.InstanceName(), true, false)
					}
				}
			}
		}
		for _, plugInfo := range up.Plugs {
			iface := repo.Interface(plugInfo.Interface)
			if iface == nil {
				return nil, fmt.Errorf("internal error: unknown interface %s", plugInfo.Interface)
			}
			if !usesMountBackend(iface) {
				continue
			}
			conns, err := repo.Connected(up.InstanceName(), plugInfo.Name)
			if err != nil {
				return nil, err
			}
			for _, cref := range conns {
				if all[cref.SlotRef.Snap] != nil {
					addAffected(cref.SlotRef.Snap, up.InstanceName(), true, false)
				}
			}
		}
	}

	return affected, nil
}

// XXX: this is too wide and affects all commonInterface-based interfaces; we
// need metadata on the relevant interfaces.
func usesMountBackend(iface interfaces.Interface) bool {
	type definer1 interface {
		MountConnectedSlot(*mount.Specification, *interfaces.ConnectedPlug, *interfaces.ConnectedSlot) error
	}
	type definer2 interface {
		MountConnectedPlug(*mount.Specification, *interfaces.ConnectedPlug, *interfaces.ConnectedSlot) error
	}
	type definer3 interface {
		MountPermanentPlug(*mount.Specification, *snap.PlugInfo) error
	}
	type definer4 interface {
		MountPermanentSlot(*mount.Specification, *snap.SlotInfo) error
	}

	if _, ok := iface.(definer1); ok {
		return true
	}
	if _, ok := iface.(definer2); ok {
		return true
	}
	if _, ok := iface.(definer3); ok {
		return true
	}
	if _, ok := iface.(definer4); ok {
		return true
	}
	return false
}

// createGateAutoRefreshHooks creates gate-auto-refresh hooks for all affectedSnaps.
// The hooks will have their context data set from affectedSnapInfo flags (base, restart).
// Hook tasks will be chained to run sequentially.
func createGateAutoRefreshHooks(st *state.State, affectedSnaps map[string]*affectedSnapInfo) *state.TaskSet {
	ts := state.NewTaskSet()
	var prev *state.Task
	for snapName, affected := range affectedSnaps {
		hookTask := SetupGateAutoRefreshHook(st, snapName, affected.Base, affected.Restart)
		// XXX: it should be fine to run the hooks in parallel
		if prev != nil {
			hookTask.WaitFor(prev)
		}
		ts.AddTask(hookTask)
		prev = hookTask
	}
	return ts
}
