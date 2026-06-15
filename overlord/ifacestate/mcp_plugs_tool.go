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

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
)

const toolListPlugs = "snap_list_plugs"

type listPlugsTool struct{}

type listPlugsArgs struct {
	SnapName       string `json:"snap_name,omitempty" mcp:"description=Optional filter plugs to a specific snap name."`
	Interface      string `json:"interface,omitempty" mcp:"description=Optional filter plugs by interface name."`
	IncludeDetails bool   `json:"include_details,omitempty" mcp:"description=Optional flag to include detailed plug fields. Defaults to false."`
}

type plugSlotResponse struct {
	SnapName   string          `json:"snap_name"`
	Name       string          `json:"name"`
	Interface  string          `json:"interface"`
	Label      string          `json:"label,omitempty"`
	Attrs      *map[string]any `json:"attrs,omitempty"`
	Apps       []string        `json:"apps,omitempty"`
	Unscoped   bool            `json:"unscoped,omitempty"`
	HotplugKey string          `json:"hotplug_key,omitempty"`
}

type listPlugsResult struct {
	Plugs []plugSlotResponse `json:"plugs"`
}

var listPlugsToolDescriptor = mcp.ToolDescriptor{
	Name:         toolListPlugs,
	Title:        "List interface plugs",
	Description:  "List interface plugs with optional snap and interface filters (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(listPlugsArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(listPlugsResult{}),
}

func (listPlugsTool) Descriptor() mcp.ToolDescriptor {
	return listPlugsToolDescriptor
}

func (listPlugsTool) ArgsType() any {
	return &listPlugsArgs{}
}

func (listPlugsTool) ValidateArgs(args any) error {
	if _, ok := args.(*listPlugsArgs); !ok {
		return fmt.Errorf("invalid typed args for list plugs tool")
	}
	return nil
}

func (listPlugsTool) ResultType() any {
	return &listPlugsResult{}
}

func (listPlugsTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	repo, err := repoFromState(st)
	if err != nil {
		return nil, fmt.Errorf("cannot list plugs: %w", err)
	}

	filterArgs, ok := args.(*listPlugsArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for list plugs tool")
	}

	plugs := repo.AllPlugs(filterArgs.Interface)
	result := listPlugsResult{Plugs: make([]plugSlotResponse, 0, len(plugs))}
	for _, plug := range plugs {
		if filterArgs.SnapName != "" && plug.Snap.InstanceName() != filterArgs.SnapName {
			continue
		}
		entry := plugSlotResponse{
			SnapName:  plug.Snap.InstanceName(),
			Name:      plug.Name,
			Interface: plug.Interface,
		}
		if filterArgs.IncludeDetails {
			attrs := plug.Attrs
			if attrs == nil {
				attrs = map[string]any{}
			}
			entry.Label = plug.Label
			entry.Attrs = &attrs
			entry.Apps = sortedAppNames(plug.Apps)
			entry.Unscoped = plug.Unscoped
		}
		result.Plugs = append(result.Plugs, entry)
	}
	return result, nil
}

func (listPlugsTool) Validate(args map[string]any) error {
	_, err := mcp.ToolArgsFromMap[listPlugsArgs](args)
	return err
}

func (listPlugsTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[listPlugsArgs](args)
	if err != nil {
		return nil, err
	}
	return listPlugsTool{}.CallWithArgs(ctx, st, parsedArgs)
}
