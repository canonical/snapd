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
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

const toolListSnaps = "snap_list_snaps"

type listSnapsTool struct{}

// snapResult is an internal representation used to build MCP responses.
type snapResult struct {
	info      *snap.Info
	channel   string
	installed bool
	status    string
}

type listSnapsArgs struct {
	Name string `json:"name,omitempty" mcp:"description=Optional filter by snap name (case-insensitive substring match)."`
}

type snapSummary struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Revision  int    `json:"revision"`
	Channel   string `json:"channel"`
	Installed bool   `json:"installed"`
	Developer string `json:"developer"`
	Status    string `json:"status"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
}

type listSnapsResult struct {
	Snaps []snapSummary `json:"snaps"`
}

var listSnapsToolDescriptor = mcp.ToolDescriptor{
	Name:         toolListSnaps,
	Title:        "List installed snaps",
	Description:  "List all installed snaps with basic information (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(listSnapsArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(listSnapsResult{}),
}

func (listSnapsTool) Descriptor() mcp.ToolDescriptor {
	return listSnapsToolDescriptor
}

func (listSnapsTool) ArgsType() any {
	return &listSnapsArgs{}
}

func (listSnapsTool) ValidateArgs(args any) error {
	if _, ok := args.(*listSnapsArgs); !ok {
		return fmt.Errorf("invalid typed args for list snaps tool")
	}
	return nil
}

func (listSnapsTool) ResultType() any {
	return &listSnapsResult{}
}

func (listSnapsTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	snaps, err := listSnapsFromState(st)
	if err != nil {
		return nil, fmt.Errorf("cannot list snaps: %w", err)
	}

	filterArgs, ok := args.(*listSnapsArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for list snaps tool")
	}

	return listSnapsToResult(snaps, filterArgs.Name), nil
}

func listSnapsToResult(snaps []snapResult, nameFilter string) listSnapsResult {
	filter := strings.ToLower(nameFilter)

	output := listSnapsResult{Snaps: make([]snapSummary, 0, len(snaps))}
	for _, snapState := range snaps {
		name := snapState.info.InstanceName()
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		output.Snaps = append(output.Snaps, snapSummary{
			Name:      name,
			Version:   snapState.info.Version,
			Revision:  snapState.info.Revision.N,
			Channel:   snapState.channel,
			Installed: snapState.installed,
			Developer: snapState.info.Publisher.Username,
			Status:    snapState.status,
			Title:     snapState.info.Title(),
			Summary:   snapState.info.Summary(),
		})
	}

	return output
}

// listSnapsFromState returns installed snaps from overlord state.
func listSnapsFromState(st *state.State) ([]snapResult, error) {
	st.Lock()
	defer st.Unlock()

	snapStates, err := All(st)
	if err != nil {
		return nil, fmt.Errorf("cannot get snaps: %w", err)
	}

	result := make([]snapResult, 0, len(snapStates))
	for snapName, snapst := range snapStates {
		info, err := snapst.CurrentInfo()
		if err == ErrNoCurrent {
			// Skip snaps without current info (removed but with state)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read snap details for %q: %w", snapName, err)
		}

		result = append(result, snapResult{
			info:      info,
			channel:   snapst.TrackingChannel,
			installed: snapst.Active,
			status:    statusFromSnapState(snapst),
		})
	}

	return result, nil
}

// statusFromSnapState returns the status string for a snap from its SnapState.
func statusFromSnapState(snapst *SnapState) string {
	if snapst.Active {
		return "active"
	}
	return "installed"
}

// snapFromState returns information about a specific snap from overlord state.
func snapFromState(st *state.State, name string) (*snapResult, error) {
	st.Lock()
	defer st.Unlock()

	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, fmt.Errorf("cannot find snap %q", name)
		}
		return nil, fmt.Errorf("cannot consult state for %q: %w", name, err)
	}

	info, err := snapst.CurrentInfo()
	if err == ErrNoCurrent {
		return nil, fmt.Errorf("cannot find active revision for %q", name)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read snap details for %q: %w", name, err)
	}

	return &snapResult{
		info:      info,
		channel:   snapst.TrackingChannel,
		installed: snapst.Active,
		status:    statusFromSnapState(&snapst),
	}, nil
}

// snapToMap converts a snapResult to a map suitable for JSON serialization.
func snapToMap(snap *snapResult) map[string]any {
	return map[string]any{
		"name":      snap.info.InstanceName(),
		"version":   snap.info.Version,
		"revision":  snap.info.Revision.N,
		"channel":   snap.channel,
		"installed": snap.installed,
		"developer": snap.info.Publisher.Username,
		"status":    snap.status,
		"title":     snap.info.Title(),
		"summary":   snap.info.Summary(),
	}
}

func (tool listSnapsTool) Validate(args map[string]any) error {
	parsedArgs, err := mcp.ToolArgsFromMap[listSnapsArgs](args)
	if err != nil {
		return err
	}
	return tool.ValidateArgs(parsedArgs)
}

func (tool listSnapsTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[listSnapsArgs](args)
	if err != nil {
		return nil, err
	}
	return tool.CallWithArgs(ctx, st, parsedArgs)
}
