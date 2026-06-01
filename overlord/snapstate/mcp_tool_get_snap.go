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
	"strings"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
)

const toolGetSnap = "snap_get_snap"

type getSnapTool struct{}

type getSnapArgs struct {
	SnapName string `json:"snap_name" mcp:"description=Name of the snap to query."`
}

type getSnapResult = snapSummary

var getSnapToolDescriptor = mcp.ToolDescriptor{
	Name:         toolGetSnap,
	Title:        "Get snap details",
	Description:  "Get detailed information about a specific snap by name (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(getSnapArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(getSnapResult{}),
}

func (getSnapTool) Descriptor() mcp.ToolDescriptor {
	return getSnapToolDescriptor
}

func (getSnapTool) ArgsType() any {
	return &getSnapArgs{}
}

func (getSnapTool) ValidateArgs(args any) error {
	v, ok := args.(*getSnapArgs)
	if !ok {
		return fmt.Errorf("invalid typed args for get snap tool")
	}
	if strings.TrimSpace(v.SnapName) == "" {
		return fmt.Errorf("snap_name is required")
	}
	return nil
}

func (getSnapTool) ResultType() any {
	return &getSnapResult{}
}

func (getSnapTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	filterArgs, ok := args.(*getSnapArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for get snap tool")
	}

	snapResult, err := snapFromState(st, filterArgs.SnapName)
	if err != nil {
		return nil, fmt.Errorf("cannot get snap %q: %w", filterArgs.SnapName, err)
	}

	return snapSummary{
		Name:      snapResult.info.InstanceName(),
		Version:   snapResult.info.Version,
		Revision:  snapResult.info.Revision.N,
		Channel:   snapResult.channel,
		Installed: snapResult.installed,
		Developer: snapResult.info.Publisher.Username,
		Status:    snapResult.status,
		Title:     snapResult.info.Title(),
		Summary:   snapResult.info.Summary(),
	}, nil
}

func (getSnapTool) Validate(args map[string]any) error {
	parsedArgs, err := mcp.ToolArgsFromMap[getSnapArgs](args)
	if err != nil {
		return err
	}
	return getSnapTool{}.ValidateArgs(parsedArgs)
}

func (getSnapTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[getSnapArgs](args)
	if err != nil {
		return nil, err
	}
	return getSnapTool{}.CallWithArgs(ctx, st, parsedArgs)
}
