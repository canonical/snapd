// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package daemon

import (
	"net/http"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate"
)

var connectionsCmd = &Command{
	Path:   "/v2/connections",
	UserOK: true,
	GET:    getConnections,
}

type collectFilter struct {
	snapName     string
	plugSlotName string
	ifaceName    string
	connected    bool
}

func (c *collectFilter) plugMatches(plug *interfaces.PlugRef, connectedSlots []interfaces.SlotRef) bool {
	for _, slot := range connectedSlots {
		if c.slotMatches(&slot, nil) {
			return true
		}
	}
	if c.snapName != "" && plug.Snap != c.snapName {
		return false
	}
	if c.plugSlotName != "" && plug.Name != c.plugSlotName {
		return false
	}
	return true
}

func (c *collectFilter) slotMatches(slot *interfaces.SlotRef, connectedPlugs []interfaces.PlugRef) bool {
	for _, plug := range connectedPlugs {
		if c.plugMatches(&plug, nil) {
			return true
		}
	}
	if c.snapName != "" && slot.Snap != c.snapName {
		return false
	}
	if c.plugSlotName != "" && slot.Name != c.plugSlotName {
		return false
	}
	return true
}

func (c *collectFilter) ifaceMatches(ifaceName string) bool {
	if c.ifaceName != "" && c.ifaceName != ifaceName {
		return false
	}
	return true
}

func collectConnections(ifaceMgr *ifacestate.InterfaceManager, filter collectFilter) connectionsJSON {
	repo := ifaceMgr.Repository()
	ifaces := repo.Interfaces()

	var connsjson connectionsJSON
	plugConns := map[string][]interfaces.SlotRef{}
	slotConns := map[string][]interfaces.PlugRef{}

	for _, cref := range ifaces.Connections {
		if !filter.plugMatches(&cref.PlugRef, nil) && !filter.slotMatches(&cref.SlotRef, nil) {
			continue
		}
		plugRef := interfaces.PlugRef{Snap: cref.PlugRef.Snap, Name: cref.PlugRef.Name}
		slotRef := interfaces.SlotRef{Snap: cref.SlotRef.Snap, Name: cref.SlotRef.Name}
		plugID := plugRef.String()
		slotID := slotRef.String()
		plugConns[plugID] = append(plugConns[plugID], slotRef)
		slotConns[slotID] = append(slotConns[slotID], plugRef)
	}

	for _, plug := range ifaces.Plugs {
		plugRef := interfaces.PlugRef{Snap: plug.Snap.InstanceName(), Name: plug.Name}
		connectedSlots, connected := plugConns[plugRef.String()]
		if !connected && filter.connected {
			continue
		}
		if !filter.ifaceMatches(plug.Interface) || !filter.plugMatches(&plugRef, connectedSlots) {
			continue
		}
		var apps []string
		for _, app := range plug.Apps {
			apps = append(apps, app.Name)
		}
		pj := &plugJSON{
			Snap:        plugRef.Snap,
			Name:        plugRef.Name,
			Interface:   plug.Interface,
			Attrs:       plug.Attrs,
			Apps:        apps,
			Label:       plug.Label,
			Connections: connectedSlots,
		}
		connsjson.Plugs = append(connsjson.Plugs, pj)
	}
	for _, slot := range ifaces.Slots {
		slotRef := interfaces.SlotRef{Snap: slot.Snap.InstanceName(), Name: slot.Name}
		connectedPlugs, connected := slotConns[slotRef.String()]
		if !connected && filter.connected {
			continue
		}
		if !filter.ifaceMatches(slot.Interface) || !filter.slotMatches(&slotRef, connectedPlugs) {
			continue
		}
		var apps []string
		for _, app := range slot.Apps {
			apps = append(apps, app.Name)
		}
		sj := &slotJSON{
			Snap:        slotRef.Snap,
			Name:        slotRef.Name,
			Interface:   slot.Interface,
			Attrs:       slot.Attrs,
			Apps:        apps,
			Label:       slot.Label,
			Connections: connectedPlugs,
		}
		connsjson.Slots = append(connsjson.Slots, sj)
	}
	return connsjson
}

func getConnections(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	snapName := query.Get("snap")
	plugSlotName := query.Get("name")
	ifaceName := query.Get("interface")
	qselect := query.Get("select")
	if qselect != "all" && qselect != "" {
		return BadRequest("unsupported select qualifier")
	}
	onlyConnected := qselect == ""

	connsjson := collectConnections(c.d.overlord.InterfaceManager(), collectFilter{
		snapName:     snapName,
		plugSlotName: plugSlotName,
		ifaceName:    ifaceName,
		connected:    onlyConnected,
	})
	return SyncResponse(connsjson, nil)
}
