// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package patch

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type connStatePatch7 struct {
	Auto             bool                   `json:"auto,omitempty"`
	ByGadget         bool                   `json:"by-gadget,omitempty"`
	Interface        string                 `json:"interface,omitempty"`
	Undesired        bool                   `json:"undesired,omitempty"`
	StaticPlugAttrs  map[string]interface{} `json:"plug-static,omitempty"`
	DynamicPlugAttrs map[string]interface{} `json:"plug-dynamic,omitempty"`
	StaticSlotAttrs  map[string]interface{} `json:"slot-static,omitempty"`
	DynamicSlotAttrs map[string]interface{} `json:"slot-dynamic,omitempty"`
}

func init() {
	patches[7] = patch7
}

// processConns updates conns map and augments it with plug-static and slot-static attributes from current snap info.
// NOTE: missing snap or missing plugs/slots are ignored and not reported as errors as we might have stale connections
// and ifacemgr deals with them (i.e. discards) on startup; we want to process all good slots and plugs here.
func processConns(conns map[string]connStatePatch7, infos map[string]*snap.Info) (bool, error) {
	var updated bool
	for id, conn := range conns {
		if conn.StaticPlugAttrs != nil || conn.StaticSlotAttrs != nil {
			continue
		}

		// undesired connections have all their attributes dropped, so don't set them
		if conn.Undesired {
			continue
		}

		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return false, err
		}

		var ok bool
		var plugSnapInfo, slotSnapInfo *snap.Info

		// read current snap info from disk and keep it around in infos map
		if plugSnapInfo, ok = infos[connRef.PlugRef.Snap]; !ok {
			plugSnapInfo, err = snap.ReadCurrentInfo(connRef.PlugRef.Snap)
			if err == nil {
				infos[connRef.PlugRef.Snap] = plugSnapInfo
			}
		}
		if slotSnapInfo, ok = infos[connRef.SlotRef.Snap]; !ok {
			slotSnapInfo, err = snap.ReadCurrentInfo(connRef.SlotRef.Snap)
			if err == nil {
				infos[connRef.SlotRef.Snap] = slotSnapInfo
			}
		}

		if slotSnapInfo != nil {
			if slot, ok := slotSnapInfo.Slots[connRef.SlotRef.Name]; ok && slot.Attrs != nil {
				conn.StaticSlotAttrs = slot.Attrs
				updated = true
			}
		}

		if plugSnapInfo != nil {
			if plug, ok := plugSnapInfo.Plugs[connRef.PlugRef.Name]; ok && plug.Attrs != nil {
				conn.StaticPlugAttrs = plug.Attrs
				updated = true
			}
		}

		conns[id] = conn
	}

	return updated, nil
}

// patch7:
//  - add static plug and slot attributes to connections that miss them. Attributes are read from current snap info.
func patch7(st *state.State) error {
	infos := make(map[string]*snap.Info)

	// update all pending "discard-conns" tasks as they may keep connection data in "removed".
	for _, task := range st.Tasks() {
		if task.Change().Status().Ready() {
			continue
		}

		var removed map[string]connStatePatch7
		if task.Kind() == "discard-conns" {
			err := task.Get("removed", &removed)
			if err == state.ErrNoState {
				continue
			}
			if err != nil {
				return err
			}
		}

		updated, err := processConns(removed, infos)
		if err != nil {
			return err
		}

		if updated {
			task.Set("removed", removed)
		}
	}

	// update conns
	var conns map[string]connStatePatch7
	err := st.Get("conns", &conns)
	if err == state.ErrNoState {
		// no connections to process
		return nil
	}
	if err != nil {
		return err
	}

	updated, err := processConns(conns, infos)
	if err != nil {
		return err
	}
	if updated {
		st.Set("conns", conns)
	}

	return nil
}
