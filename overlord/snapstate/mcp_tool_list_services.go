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

package snapstate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
)

const toolListServices = "snap_list_services"

type listServicesTool struct{}

type listServicesArgs struct {
	SnapName    string `json:"snap_name,omitempty" mcp:"description=Optional filter by snap instance name."`
	ServiceName string `json:"service_name,omitempty" mcp:"description=Optional case-insensitive substring filter on <snap>.<app> service name."`
}

type listServicesItem struct {
	ServiceName string `json:"service_name"`
	SnapName    string `json:"snap_name"`
	AppName     string `json:"app_name"`
	Daemon      string `json:"daemon"`
	DaemonScope string `json:"daemon_scope"`
	ServiceUnit string `json:"service_unit"`
}

type listServicesResult struct {
	Services []listServicesItem `json:"services"`
}

var listServicesToolDescriptor = mcp.ToolDescriptor{
	Name:         toolListServices,
	Title:        "List snap services",
	Description:  "List services provided by installed snaps (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(listServicesArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(listServicesResult{}),
}

func (listServicesTool) Descriptor() mcp.ToolDescriptor {
	return listServicesToolDescriptor
}

func (listServicesTool) ArgsType() any {
	return &listServicesArgs{}
}

func (listServicesTool) ValidateArgs(args any) error {
	_, ok := args.(*listServicesArgs)
	if !ok {
		return fmt.Errorf("invalid typed args for list services tool")
	}
	return nil
}

func (listServicesTool) ResultType() any {
	return &listServicesResult{}
}

func (listServicesTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	filterArgs, ok := args.(*listServicesArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for list services tool")
	}

	snapNameFilter := strings.TrimSpace(filterArgs.SnapName)
	serviceNameFilter := strings.ToLower(strings.TrimSpace(filterArgs.ServiceName))

	services, err := listServicesFromState(st, snapNameFilter, serviceNameFilter)
	if err != nil {
		return nil, fmt.Errorf("cannot list services: %w", err)
	}

	return listServicesResult{Services: services}, nil
}

func (listServicesTool) Validate(args map[string]any) error {
	_, err := mcp.ToolArgsFromMap[listServicesArgs](args)
	return err
}

func (listServicesTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[listServicesArgs](args)
	if err != nil {
		return nil, err
	}
	return listServicesTool{}.CallWithArgs(ctx, st, parsedArgs)
}

func listServicesFromState(st *state.State, snapNameFilter, serviceNameFilter string) ([]listServicesItem, error) {
	st.Lock()
	defer st.Unlock()

	snapStates, err := All(st)
	if err != nil {
		return nil, fmt.Errorf("cannot get snaps: %w", err)
	}

	result := make([]listServicesItem, 0)
	for snapName, snapst := range snapStates {
		if snapNameFilter != "" && snapName != snapNameFilter {
			continue
		}

		info, err := snapst.CurrentInfo()
		if err == ErrNoCurrent {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read snap details for %q: %w", snapName, err)
		}

		for _, app := range info.Apps {
			if !app.IsService() {
				continue
			}
			serviceName := fmt.Sprintf("%s.%s", info.InstanceName(), app.Name)
			if serviceNameFilter != "" && !strings.Contains(strings.ToLower(serviceName), serviceNameFilter) {
				continue
			}
			result = append(result, listServicesItem{
				ServiceName: serviceName,
				SnapName:    info.InstanceName(),
				AppName:     app.Name,
				Daemon:      string(app.Daemon),
				DaemonScope: string(app.DaemonScope),
				ServiceUnit: app.ServiceName(),
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ServiceName < result[j].ServiceName
	})

	return result, nil
}
