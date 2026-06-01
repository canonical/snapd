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
	"time"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

const toolListChanges = "snap_list_changes"

type listChangesTool struct{}

type listChangesArgs struct {
	Select   string     `json:"select,omitempty" mcp:"description=Optional readiness filter: all, in-progress, or ready (default: in-progress)."`
	SnapName string     `json:"snap_name,omitempty" mcp:"description=Optional filter to include only changes affecting a given snap."`
	Kind     string     `json:"kind,omitempty" mcp:"description=Optional filter to include only changes with the given kind (case-insensitive)."`
	Status   string     `json:"status,omitempty" mcp:"description=Optional filter to include only changes with the given status (case-insensitive)."`
	Since    *time.Time `json:"since,omitempty" mcp:"description=Optional inclusive lower bound on change spawn time in RFC3339 format."`
	Until    *time.Time `json:"until,omitempty" mcp:"description=Optional inclusive upper bound on change spawn time in RFC3339 format."`
}

type listChangesItem struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Summary   string     `json:"summary"`
	Status    string     `json:"status"`
	Ready     bool       `json:"ready"`
	SpawnTime time.Time  `json:"spawn_time"`
	ReadyTime *time.Time `json:"ready_time,omitempty"`
	SnapNames []string   `json:"snap_names,omitempty"`
	Err       string     `json:"err,omitempty"`
}

type listChangesResult struct {
	Changes []listChangesItem `json:"changes"`
}

var listChangesSelectEnum = []string{"all", "in-progress", "ready"}

var listChangesKindEnum = []string{
	"alias",
	"auto-refresh",
	"create-recovery-system",
	"disable",
	"enable",
	"enable-snap",
	"install",
	"install-component",
	"install-snap",
	"migrate-home",
	"pre-download",
	"prefer",
	"refresh",
	"refresh-snap",
	"refresh-to-enforce",
	"remodel",
	"remove",
	"resolve-validation-sets",
	"revert",
	"snapd-refresh",
	"start-services",
	"stop-services",
	"switch-snap",
	"transition-to-snapd-snap",
	"transition-ubuntu-core",
	"try",
	"unalias",
	"update-snap",
}

var listChangesStatusEnum = []string{
	"do",
	"doing",
	"done",
	"abort",
	"error",
	"hold",
	"undo",
	"undoing",
	"undone",
	"wait",
	"fail",
	"failed",
}

var listChangesToolDescriptor = mcp.ToolDescriptor{
	Name:         toolListChanges,
	Title:        "List snap changes",
	Description:  "List snap changes with optional readiness, snap-name, kind, status, and date filters (read-only; task details are omitted).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(listChangesArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(listChangesResult{}),
}

func (listChangesTool) Descriptor() mcp.ToolDescriptor {
	return listChangesToolDescriptor
}

func (listChangesTool) ArgsType() any {
	return &listChangesArgs{}
}

func (listChangesTool) ValidateArgs(args any) error {
	v, ok := args.(*listChangesArgs)
	if !ok {
		return fmt.Errorf("invalid typed args for list changes tool")
	}
	if v.Select != "" && !strutil.ListContains(listChangesSelectEnum, v.Select) {
		return fmt.Errorf("select should be one of: all,in-progress,ready")
	}
	if v.Kind != "" && !strutil.ListContainsFold(listChangesKindEnum, v.Kind) {
		return fmt.Errorf("kind should be one of: %s", strings.Join(listChangesKindEnum, ","))
	}
	if v.Status != "" && !strutil.ListContainsFold(listChangesStatusEnum, v.Status) {
		return fmt.Errorf("status should be one of: %s", strings.Join(listChangesStatusEnum, ","))
	}

	if v.Since != nil && v.Until != nil && v.Since.After(*v.Until) {
		return fmt.Errorf("since must not be after until")
	}

	return nil
}

func (listChangesTool) ResultType() any {
	return &listChangesResult{}
}

func (listChangesTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	filterArgs, ok := args.(*listChangesArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for list changes tool")
	}

	selectValue := "in-progress"
	if filterArgs.Select != "" {
		selectValue = filterArgs.Select
	}

	var snapNameFilter string
	if filterArgs.SnapName != "" {
		snapNameFilter = filterArgs.SnapName
	}

	var kindFilter string
	if filterArgs.Kind != "" {
		kindFilter = strings.ToLower(filterArgs.Kind)
	}

	var statusFilter string
	if filterArgs.Status != "" {
		statusFilter = filterArgs.Status
	}

	var sinceTime time.Time
	if filterArgs.Since != nil {
		sinceTime = *filterArgs.Since
	}

	var untilTime time.Time
	if filterArgs.Until != nil {
		untilTime = *filterArgs.Until
	}

	if !sinceTime.IsZero() && !untilTime.IsZero() && sinceTime.After(untilTime) {
		return nil, errors.New("since must not be after until")
	}

	st.Lock()
	defer st.Unlock()

	changes := st.Changes()
	items := make([]listChangesItem, 0, len(changes))
	for _, chg := range changes {
		if !matchesChangeSelect(chg, selectValue) {
			continue
		}
		if kindFilter != "" && strings.ToLower(chg.Kind()) != kindFilter {
			continue
		}
		if statusFilter != "" && !matchesChangeStatus(chg, statusFilter) {
			continue
		}
		spawnTime := chg.SpawnTime()
		if !sinceTime.IsZero() && spawnTime.Before(sinceTime) {
			continue
		}
		if !untilTime.IsZero() && spawnTime.After(untilTime) {
			continue
		}
		snapNames := changeSnapNames(chg)
		if snapNameFilter != "" && !containsSnapName(snapNames, snapNameFilter) {
			continue
		}

		items = append(items, changeToItem(chg, snapNames))
	}

	return listChangesResult{Changes: items}, nil
}

func (listChangesTool) Validate(args map[string]any) error {
	_, err := mcp.ToolArgsFromMap[listChangesArgs](args)
	return err
}

func (listChangesTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[listChangesArgs](args)
	if err != nil {
		return nil, err
	}
	return listChangesTool{}.CallWithArgs(ctx, st, parsedArgs)
}

func matchesChangeSelect(chg *state.Change, selectValue string) bool {
	switch selectValue {
	case "all":
		return true
	case "ready":
		return chg.IsReady()
	case "in-progress":
		return !chg.IsReady()
	default:
		return false
	}
}

func matchesChangeStatus(chg *state.Change, statusFilter string) bool {
	current := strings.ToLower(chg.Status().String())
	filter := strings.ToLower(strings.TrimSpace(statusFilter))
	if filter == "failed" || filter == "fail" {
		filter = "error"
	}
	return current == filter
}

func changeSnapNames(chg *state.Change) []string {
	var snapNames []string
	if err := chg.Get("snap-names", &snapNames); err != nil {
		return nil
	}
	return snapNames
}

func containsSnapName(snapNames []string, wanted string) bool {
	for _, name := range snapNames {
		snapName, _ := snap.SplitSnapApp(name)
		if snapName == wanted {
			return true
		}
	}
	return false
}

func changeToItem(chg *state.Change, snapNames []string) listChangesItem {
	status := chg.Status()
	item := listChangesItem{
		ID:        chg.ID(),
		Kind:      chg.Kind(),
		Summary:   chg.Summary(),
		Status:    status.String(),
		Ready:     status.Ready(),
		SpawnTime: chg.SpawnTime(),
	}
	if readyTime := chg.ReadyTime(); !readyTime.IsZero() {
		item.ReadyTime = &readyTime
	}
	if len(snapNames) > 0 {
		item.SnapNames = snapNames
	}
	if err := chg.Err(); err != nil {
		item.Err = err.Error()
	}
	return item
}

func changeToMap(chg *state.Change, snapNames []string) map[string]any {
	status := chg.Status()
	result := map[string]any{
		"id":         chg.ID(),
		"kind":       chg.Kind(),
		"summary":    chg.Summary(),
		"status":     status.String(),
		"ready":      status.Ready(),
		"spawn_time": chg.SpawnTime(),
	}
	if readyTime := chg.ReadyTime(); !readyTime.IsZero() {
		result["ready_time"] = readyTime
	}
	if len(snapNames) > 0 {
		result["snap_names"] = snapNames
	}
	if err := chg.Err(); err != nil {
		result["err"] = err.Error()
	}
	return result
}
