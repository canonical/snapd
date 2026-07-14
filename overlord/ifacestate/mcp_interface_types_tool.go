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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
)

const toolListInterfaceTypes = "snap_list_interface_types"

type listInterfaceTypesTool struct{}

type listInterfaceTypesArgs struct {
	Name           string `json:"name,omitempty" mcp:"description=Optional case-insensitive substring filter by interface name."`
	IncludeDetails bool   `json:"include_details,omitempty" mcp:"description=Optional flag to include base policy and implemented backends. Defaults to false."`
}

type basePolicyResponse struct {
	Plugs string `json:"plugs"`
	Slots string `json:"slots"`
}

type interfaceTypeResponse struct {
	Name                string              `json:"name"`
	Summary             string              `json:"summary,omitempty"`
	BasePolicy          *basePolicyResponse `json:"base_policy,omitempty"`
	ImplementedBackends []string            `json:"implemented_backends,omitempty"`
}

type listInterfaceTypesResult struct {
	InterfaceTypes []interfaceTypeResponse `json:"interface_types"`
}

var listInterfaceTypesToolDescriptor = mcp.ToolDescriptor{
	Name:         toolListInterfaceTypes,
	Title:        "List interface types",
	Description:  "List known interface types with summary, base policy snippets, and implemented security backends (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(listInterfaceTypesArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(listInterfaceTypesResult{}),
}

func (listInterfaceTypesTool) Descriptor() mcp.ToolDescriptor {
	return listInterfaceTypesToolDescriptor
}

func (listInterfaceTypesTool) ArgsType() any {
	return &listInterfaceTypesArgs{}
}

func (listInterfaceTypesTool) ValidateArgs(args any) error {
	if _, ok := args.(*listInterfaceTypesArgs); !ok {
		return fmt.Errorf("invalid typed args for list interface types tool")
	}
	return nil
}

func (listInterfaceTypesTool) ResultType() any {
	return &listInterfaceTypesResult{}
}

func (listInterfaceTypesTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	repo, err := repoFromState(st)
	if err != nil {
		return nil, fmt.Errorf("cannot list interface types: %w", err)
	}

	filterArgs, ok := args.(*listInterfaceTypesArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for list interface types tool")
	}

	var nameFilter string
	if filterArgs.Name != "" {
		nameFilter = strings.ToLower(filterArgs.Name)
	}

	includeDetails := filterArgs.IncludeDetails

	ifaces := repo.AllInterfaces()
	result := listInterfaceTypesResult{InterfaceTypes: make([]interfaceTypeResponse, 0, len(ifaces))}
	for _, iface := range ifaces {
		ifaceName := iface.Name()
		if nameFilter != "" && !strings.Contains(strings.ToLower(ifaceName), nameFilter) {
			continue
		}

		si := interfaces.StaticInfoOf(iface)
		entry := interfaceTypeResponse{Name: ifaceName}
		if includeDetails {
			entry.Summary = si.Summary
			entry.BasePolicy = &basePolicyResponse{Plugs: si.BaseDeclarationPlugs, Slots: si.BaseDeclarationSlots}
			entry.ImplementedBackends = implementedBackends(iface)
		}
		result.InterfaceTypes = append(result.InterfaceTypes, entry)
	}

	return result, nil
}

func (listInterfaceTypesTool) Validate(args map[string]any) error {
	_, err := mcp.ToolArgsFromMap[listInterfaceTypesArgs](args)
	return err
}

func (listInterfaceTypesTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[listInterfaceTypesArgs](args)
	if err != nil {
		return nil, err
	}
	return listInterfaceTypesTool{}.CallWithArgs(ctx, st, parsedArgs)
}
