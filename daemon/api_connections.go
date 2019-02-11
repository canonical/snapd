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
	"sort"

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
	snapName  string
	ifaceName string
	connected bool
}

func (c *collectFilter) plugOrConnectedSlotMatches(plug *interfaces.PlugRef, connectedSlots []interfaces.SlotRef) bool {
	for _, slot := range connectedSlots {
		if c.slotOrConnectedPlugMatches(&slot, nil) {
			return true
		}
	}
	if c.snapName != "" && plug.Snap != c.snapName {
		return false
	}
	return true
}

func (c *collectFilter) slotOrConnectedPlugMatches(slot *interfaces.SlotRef, connectedPlugs []interfaces.PlugRef) bool {
	for _, plug := range connectedPlugs {
		if c.plugOrConnectedSlotMatches(&plug, nil) {
			return true
		}
	}
	if c.snapName != "" && slot.Snap != c.snapName {
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

type bySlotRef []interfaces.SlotRef

func (b bySlotRef) Len() int      { return len(b) }
func (b bySlotRef) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b bySlotRef) Less(i, j int) bool {
	return b[i].SortsBefore(b[j])
}

type byPlugRef []interfaces.PlugRef

func (b byPlugRef) Len() int      { return len(b) }
func (b byPlugRef) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byPlugRef) Less(i, j int) bool {
	return b[i].SortsBefore(b[j])
}

func collectConnections(ifaceMgr *ifacestate.InterfaceManager, filter collectFilter) (*connectionsJSON, error) {
	repo := ifaceMgr.Repository()
	ifaces := repo.Interfaces()

	var connsjson connectionsJSON
	var connStates map[string]ifacestate.ConnectionState
	plugConns := map[string][]interfaces.SlotRef{}
	slotConns := map[string][]interfaces.PlugRef{}

	var err error
	connStates, err = ifaceMgr.ConnectionStates()
	if err != nil {
		return nil, err
	}

	connsjson.Established = make([]connectionJSON, 0, len(connStates))
	connsjson.Plugs = make([]*plugJSON, 0, len(ifaces.Plugs))
	connsjson.Slots = make([]*slotJSON, 0, len(ifaces.Slots))

	for crefStr, cstate := range connStates {
		if cstate.Undesired && filter.connected {
			continue
		}
		cref, err := interfaces.ParseConnRef(crefStr)
		if err != nil {
			return nil, err
		}
		if !filter.plugOrConnectedSlotMatches(&cref.PlugRef, nil) && !filter.slotOrConnectedPlugMatches(&cref.SlotRef, nil) {
			continue
		}
		if !filter.ifaceMatches(cstate.Interface) {
			continue
		}
		plugRef := interfaces.PlugRef{Snap: cref.PlugRef.Snap, Name: cref.PlugRef.Name}
		slotRef := interfaces.SlotRef{Snap: cref.SlotRef.Snap, Name: cref.SlotRef.Name}
		plugID := plugRef.String()
		slotID := slotRef.String()

		cj := connectionJSON{
			Slot:      slotRef,
			Plug:      plugRef,
			Manual:    cstate.Auto == false,
			Gadget:    cstate.ByGadget,
			Interface: cstate.Interface,
		}
		if cstate.Undesired {
			// explicitly disconnected are always manual
			cj.Manual = true
			connsjson.Undesired = append(connsjson.Undesired, cj)
		} else {
			plugConns[plugID] = append(plugConns[plugID], slotRef)
			slotConns[slotID] = append(slotConns[slotID], plugRef)

			connsjson.Established = append(connsjson.Established, cj)
		}
	}

	for _, plug := range ifaces.Plugs {
		plugRef := interfaces.PlugRef{Snap: plug.Snap.InstanceName(), Name: plug.Name}
		connectedSlots, connected := plugConns[plugRef.String()]
		if !connected && filter.connected {
			continue
		}
		if !filter.ifaceMatches(plug.Interface) || !filter.plugOrConnectedSlotMatches(&plugRef, connectedSlots) {
			continue
		}
		var apps []string
		for _, app := range plug.Apps {
			apps = append(apps, app.Name)
		}
		sort.Sort(bySlotRef(connectedSlots))
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
		if !filter.ifaceMatches(slot.Interface) || !filter.slotOrConnectedPlugMatches(&slotRef, connectedPlugs) {
			continue
		}
		var apps []string
		for _, app := range slot.Apps {
			apps = append(apps, app.Name)
		}
		sort.Sort(byPlugRef(connectedPlugs))
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
	return &connsjson, nil
}

type byCrefConnJSON []connectionJSON

func (b byCrefConnJSON) Len() int      { return len(b) }
func (b byCrefConnJSON) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byCrefConnJSON) Less(i, j int) bool {
	icj := b[i]
	jcj := b[j]
	iCref := interfaces.ConnRef{PlugRef: icj.Plug, SlotRef: icj.Slot}
	jCref := interfaces.ConnRef{PlugRef: jcj.Plug, SlotRef: jcj.Slot}
	sortsBefore := iCref.SortsBefore(&jCref)
	return sortsBefore
}

func getConnections(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	snapName := query.Get("snap")
	ifaceName := query.Get("interface")
	qselect := query.Get("select")
	if qselect != "all" && qselect != "" {
		return BadRequest("unsupported select qualifier")
	}
	onlyConnected := qselect == ""

	connsjson, err := collectConnections(c.d.overlord.InterfaceManager(), collectFilter{
		snapName:  ifacestate.RemapSnapFromRequest(snapName),
		ifaceName: ifaceName,
		connected: onlyConnected,
	})
	if err != nil {
		return InternalError("collecting connection information failed: %v", err)
	}
	sort.Sort(byCrefConnJSON(connsjson.Established))
	sort.Sort(byCrefConnJSON(connsjson.Undesired))

	return SyncResponse(connsjson, nil)
}
