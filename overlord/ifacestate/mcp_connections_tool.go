// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package ifacestate

import (
	"context"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
)

const toolListConnections = "snap_list_connections"

type listConnectionsTool struct{}

type connectionResult struct {
	ref   *interfaces.ConnRef
	state ConnectionState
}

type listConnectionsArgs struct {
	Snap string `json:"snap,omitempty" mcp:"description=Optional filter connections to a specific snap."`
}

type connectionAttrsResponse struct {
	Plug map[string]any `json:"plug"`
	Slot map[string]any `json:"slot"`
}

type connectionResponse struct {
	PlugSnap     string                  `json:"plug_snap"`
	PlugName     string                  `json:"plug_name"`
	SlotSnap     string                  `json:"slot_snap"`
	SlotName     string                  `json:"slot_name"`
	Interface    string                  `json:"interface"`
	StaticAttrs  connectionAttrsResponse `json:"static_attrs"`
	DynamicAttrs connectionAttrsResponse `json:"dynamic_attrs"`
	Manual       bool                    `json:"manual"`
}

type listConnectionsResult struct {
	Connections []connectionResponse `json:"connections"`
}

var listConnectionsToolDescriptor = mcp.ToolDescriptor{
	Name:         toolListConnections,
	Title:        "List interface connections",
	Description:  "List interface connections and slots for snaps (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(listConnectionsArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(listConnectionsResult{}),
}

func (listConnectionsTool) Descriptor() mcp.ToolDescriptor {
	return listConnectionsToolDescriptor
}

func (listConnectionsTool) ArgsType() any {
	return &listConnectionsArgs{}
}

func (listConnectionsTool) ValidateArgs(args any) error {
	if _, ok := args.(*listConnectionsArgs); !ok {
		return fmt.Errorf("invalid typed args for list connections tool")
	}
	return nil
}

func (listConnectionsTool) ResultType() any {
	return &listConnectionsResult{}
}

func (listConnectionsTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	conns, err := connectionsFromState(st)
	if err != nil {
		return nil, fmt.Errorf("cannot list connections: %w", err)
	}

	filterArgs, ok := args.(*listConnectionsArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for list connections tool")
	}

	return connectionsToResult(conns, filterArgs.Snap), nil
}

func (listConnectionsTool) Validate(args map[string]any) error {
	_, err := mcp.ToolArgsFromMap[listConnectionsArgs](args)
	return err
}

func (listConnectionsTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[listConnectionsArgs](args)
	if err != nil {
		return nil, err
	}
	return listConnectionsTool{}.CallWithArgs(ctx, st, parsedArgs)
}

func connectionsFromState(st *state.State) ([]connectionResult, error) {
	st.Lock()
	defer st.Unlock()

	connStateByRef, err := ConnectionStates(st)
	if err != nil {
		return nil, fmt.Errorf("cannot read connection state: %w", err)
	}

	result := make([]connectionResult, 0, len(connStateByRef))
	for connID, connState := range connStateByRef {
		if !connState.Active() {
			continue
		}
		connRef, err := interfaces.ParseConnRef(connID)
		if err != nil {
			return nil, fmt.Errorf("cannot parse connection reference %q: %w", connID, err)
		}
		result = append(result, connectionResult{ref: connRef, state: connState})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ref.SortsBefore(result[j].ref)
	})

	return result, nil
}

func connectionsToResult(conns []connectionResult, snapFilter string) listConnectionsResult {
	result := listConnectionsResult{Connections: make([]connectionResponse, 0, len(conns))}
	for _, conn := range conns {
		if snapFilter != "" && conn.ref.PlugRef.Snap != snapFilter && conn.ref.SlotRef.Snap != snapFilter {
			continue
		}
		result.Connections = append(result.Connections, connectionResponse{
			PlugSnap:  conn.ref.PlugRef.Snap,
			PlugName:  conn.ref.PlugRef.Name,
			SlotSnap:  conn.ref.SlotRef.Snap,
			SlotName:  conn.ref.SlotRef.Name,
			Interface: conn.state.Interface,
			StaticAttrs: connectionAttrsResponse{
				Plug: conn.state.StaticPlugAttrs,
				Slot: conn.state.StaticSlotAttrs,
			},
			DynamicAttrs: connectionAttrsResponse{
				Plug: conn.state.DynamicPlugAttrs,
				Slot: conn.state.DynamicSlotAttrs,
			},
			Manual: !conn.state.Auto && !conn.state.ByGadget,
		})
	}
	return result
}

func connectionsToMap(conns []connectionResult, snapFilter string) map[string]any {
	res := connectionsToResult(conns, snapFilter)
	return map[string]any{"connections": res.Connections}
}
