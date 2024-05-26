// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

var interfacesCmd = &Command{
	Path:        "/v2/interfaces",
	GET:         interfacesConnectionsMultiplexer,
	POST:        changeInterfaces,
	ReadAccess:  openAccess{},
	WriteAccess: authenticatedAccess{Polkit: polkitActionManageInterfaces},
}

// interfacesConnectionsMultiplexer multiplexes to either legacy (connection) or modern behavior (interfaces).
func interfacesConnectionsMultiplexer(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	qselect := query.Get("select")
	if qselect == "" {
		return getLegacyConnections(c, r, user)
	} else {
		return getInterfaces(c, r, user)
	}
}

func getInterfaces(c *Command, r *http.Request, user *auth.UserState) Response {
	// Collect query options from request arguments.
	q := r.URL.Query()
	pselect := q.Get("select")
	if pselect != "all" && pselect != "connected" {
		return BadRequest("unsupported select qualifier")
	}
	var names []string // Interface names
	namesStr := q.Get("names")
	if namesStr != "" {
		names = strings.Split(namesStr, ",")
	}
	opts := &interfaces.InfoOptions{
		Names:     names,
		Doc:       q.Get("doc") == "true",
		Plugs:     q.Get("plugs") == "true",
		Slots:     q.Get("slots") == "true",
		Connected: pselect == "connected",
	}
	// Query the interface repository (this returns []*interface.Info).
	infos := c.d.overlord.InterfaceManager().Repository().Info(opts)
	infoJSONs := make([]*interfaceJSON, 0, len(infos))

	for _, info := range infos {
		// Convert interfaces.Info into interfaceJSON
		plugs := make([]*plugJSON, 0, len(info.Plugs))
		for _, plug := range info.Plugs {
			plugs = append(plugs, &plugJSON{
				Snap:  plug.Snap.InstanceName(),
				Name:  plug.Name,
				Attrs: plug.Attrs,
				Label: plug.Label,
			})
		}
		slots := make([]*slotJSON, 0, len(info.Slots))
		for _, slot := range info.Slots {
			slots = append(slots, &slotJSON{
				Snap:  slot.Snap.InstanceName(),
				Name:  slot.Name,
				Attrs: slot.Attrs,
				Label: slot.Label,
			})
		}
		infoJSONs = append(infoJSONs, &interfaceJSON{
			Name:    info.Name,
			Summary: info.Summary,
			DocURL:  info.DocURL,
			Plugs:   plugs,
			Slots:   slots,
		})
	}
	return SyncResponse(infoJSONs)
}

func getLegacyConnections(c *Command, r *http.Request, user *auth.UserState) Response {
	connsjson := mylog.Check2(collectConnections(c.d.overlord.InterfaceManager(), collectFilter{}))

	legacyconnsjson := legacyConnectionsJSON{
		Plugs: connsjson.Plugs,
		Slots: connsjson.Slots,
	}
	return SyncResponse(legacyconnsjson)
}

// changeInterfaces controls the interfaces system.
// Plugs can be connected to and disconnected from slots.
func changeInterfaces(c *Command, r *http.Request, user *auth.UserState) Response {
	var a interfaceAction
	decoder := json.NewDecoder(r.Body)
	mylog.Check(decoder.Decode(&a))

	if a.Action == "" {
		return BadRequest("interface action not specified")
	}
	if len(a.Plugs) > 1 || len(a.Slots) > 1 {
		return NotImplemented("many-to-many operations are not implemented")
	}
	if a.Action != "connect" && a.Action != "disconnect" {
		return BadRequest("unsupported interface action: %q", a.Action)
	}
	if len(a.Plugs) == 0 || len(a.Slots) == 0 {
		return BadRequest("at least one plug and slot is required")
	}

	var summary string

	var tasksets []*state.TaskSet
	var affected []string

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	checkInstalled := func(snapName string) error {
		// empty snap name is fine, ResolveConnect/ResolveDisconnect handles it.
		if snapName == "" {
			return nil
		}
		var snapst snapstate.SnapState
		mylog.Check(snapstate.Get(st, snapName, &snapst))
		if (err == nil && !snapst.IsInstalled()) || errors.Is(err, state.ErrNoState) {
			return fmt.Errorf("snap %q is not installed", snapName)
		}
		if err == nil {
			return nil
		}
		return fmt.Errorf("internal error: cannot get state of snap %q: %v", snapName, err)
	}

	for i := range a.Plugs {
		a.Plugs[i].Snap = ifacestate.RemapSnapFromRequest(a.Plugs[i].Snap)
		mylog.Check(checkInstalled(a.Plugs[i].Snap))

	}
	for i := range a.Slots {
		a.Slots[i].Snap = ifacestate.RemapSnapFromRequest(a.Slots[i].Snap)
		mylog.Check(checkInstalled(a.Slots[i].Snap))

	}

	switch a.Action {
	case "connect":
		var connRef *interfaces.ConnRef
		repo := c.d.overlord.InterfaceManager().Repository()
		connRef = mylog.Check2(repo.ResolveConnect(a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name))
		if err == nil {
			var ts *state.TaskSet
			affected = snapNamesFromConns([]*interfaces.ConnRef{connRef})
			summary = fmt.Sprintf("Connect %s:%s to %s:%s", connRef.PlugRef.Snap, connRef.PlugRef.Name, connRef.SlotRef.Snap, connRef.SlotRef.Name)
			ts = mylog.Check2(ifacestate.Connect(st, connRef.PlugRef.Snap, connRef.PlugRef.Name, connRef.SlotRef.Snap, connRef.SlotRef.Name))
			if _, ok := err.(*ifacestate.ErrAlreadyConnected); ok {
				change := newChange(st, a.Action+"-snap", summary, nil, affected)
				change.SetStatus(state.DoneStatus)
				return AsyncResponse(nil, change.ID())
			}
			tasksets = append(tasksets, ts)
		}
	case "disconnect":
		var conns []*interfaces.ConnRef
		summary = fmt.Sprintf("Disconnect %s:%s from %s:%s", a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name)
		conns = mylog.Check2(c.d.overlord.InterfaceManager().ResolveDisconnect(a.Plugs[0].Snap, a.Plugs[0].Name, a.Slots[0].Snap, a.Slots[0].Name, a.Forget))
		if err == nil {
			if len(conns) == 0 {
				return InterfacesUnchanged("nothing to do")
			}
			repo := c.d.overlord.InterfaceManager().Repository()
			for _, connRef := range conns {
				var ts *state.TaskSet
				var conn *interfaces.Connection
				if a.Forget {
					ts = mylog.Check2(ifacestate.Forget(st, repo, connRef))
				} else {
					conn = mylog.Check2(repo.Connection(connRef))

					ts = mylog.Check2(ifacestate.Disconnect(st, conn))

				}

				ts.JoinLane(st.NewLane())
				tasksets = append(tasksets, ts)
			}
			affected = snapNamesFromConns(conns)
		}
	}

	change := newChange(st, a.Action+"-snap", summary, tasksets, affected)
	st.EnsureBefore(0)

	return AsyncResponse(nil, change.ID())
}

func snapNamesFromConns(conns []*interfaces.ConnRef) []string {
	m := make(map[string]bool)
	for _, conn := range conns {
		m[conn.PlugRef.Snap] = true
		m[conn.SlotRef.Snap] = true
	}
	l := make([]string, 0, len(m))
	for name := range m {
		l = append(l, name)
	}
	sort.Strings(l)
	return l
}
