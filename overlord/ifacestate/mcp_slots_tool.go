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

const toolListSlots = "snap_list_slots"

type listSlotsTool struct{}

type listSlotsArgs struct {
	SnapName       string `json:"snap_name,omitempty" mcp:"description=Optional filter slots to a specific snap name."`
	Interface      string `json:"interface,omitempty" mcp:"description=Optional filter slots by interface name."`
	IncludeDetails bool   `json:"include_details,omitempty" mcp:"description=Optional flag to include detailed slot fields. Defaults to false."`
}

type listSlotsResult struct {
	Slots []plugSlotResponse `json:"slots"`
}

var listSlotsToolDescriptor = mcp.ToolDescriptor{
	Name:         toolListSlots,
	Title:        "List interface slots",
	Description:  "List interface slots with optional snap and interface filters (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(listSlotsArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(listSlotsResult{}),
}

func (listSlotsTool) Descriptor() mcp.ToolDescriptor {
	return listSlotsToolDescriptor
}

func (listSlotsTool) ArgsType() any {
	return &listSlotsArgs{}
}

func (listSlotsTool) ValidateArgs(args any) error {
	if _, ok := args.(*listSlotsArgs); !ok {
		return fmt.Errorf("invalid typed args for list slots tool")
	}
	return nil
}

func (listSlotsTool) ResultType() any {
	return &listSlotsResult{}
}

func (listSlotsTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	repo, err := repoFromState(st)
	if err != nil {
		return nil, fmt.Errorf("cannot list slots: %w", err)
	}

	filterArgs, ok := args.(*listSlotsArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for list slots tool")
	}

	slots := repo.AllSlots(filterArgs.Interface)
	result := listSlotsResult{Slots: make([]plugSlotResponse, 0, len(slots))}
	for _, slot := range slots {
		if filterArgs.SnapName != "" && slot.Snap.InstanceName() != filterArgs.SnapName {
			continue
		}
		entry := plugSlotResponse{
			SnapName:  slot.Snap.InstanceName(),
			Name:      slot.Name,
			Interface: slot.Interface,
		}
		if filterArgs.IncludeDetails {
			attrs := slot.Attrs
			if attrs == nil {
				attrs = map[string]any{}
			}
			entry.Label = slot.Label
			entry.Attrs = &attrs
			entry.Apps = sortedAppNames(slot.Apps)
			entry.Unscoped = slot.Unscoped
			entry.HotplugKey = string(slot.HotplugKey)
		}
		result.Slots = append(result.Slots, entry)
	}
	return result, nil
}

func (listSlotsTool) Validate(args map[string]any) error {
	_, err := mcp.ToolArgsFromMap[listSlotsArgs](args)
	return err
}

func (listSlotsTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[listSlotsArgs](args)
	if err != nil {
		return nil, err
	}
	return listSlotsTool{}.CallWithArgs(ctx, st, parsedArgs)
}
