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
)

const toolListChangeTasks = "snap_list_change_tasks"

type listChangeTasksTool struct{}

type listChangeTasksArgs struct {
	ChangeID string `json:"change_id" mcp:"description=ID of the change to inspect."`
}

type listChangeTasksItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Summary  string `json:"summary"`
	Status   string `json:"status"`
	Progress struct {
		Label string `json:"label"`
		Done  int    `json:"done"`
		Total int    `json:"total"`
	} `json:"progress"`
	Log []string `json:"log,omitempty"`
}

type listChangeTasksResult struct {
	ChangeID string                `json:"change_id"`
	Tasks    []listChangeTasksItem `json:"tasks"`
}

var listChangeTasksToolDescriptor = mcp.ToolDescriptor{
	Name:         toolListChangeTasks,
	Title:        "List tasks for a change",
	Description:  "List all tasks associated with a specific change (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(listChangeTasksArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(listChangeTasksResult{}),
}

func (listChangeTasksTool) Descriptor() mcp.ToolDescriptor {
	return listChangeTasksToolDescriptor
}

func (listChangeTasksTool) ArgsType() any {
	return &listChangeTasksArgs{}
}

func (listChangeTasksTool) ValidateArgs(args any) error {
	v, ok := args.(*listChangeTasksArgs)
	if !ok {
		return fmt.Errorf("invalid typed args for list change tasks tool")
	}
	if strings.TrimSpace(v.ChangeID) == "" {
		return fmt.Errorf("change_id must not be empty")
	}
	return nil
}

func (listChangeTasksTool) ResultType() any {
	return &listChangeTasksResult{}
}

func (listChangeTasksTool) CallWithArgs(_ context.Context, st *state.State, args any) (any, error) {
	filterArgs, ok := args.(*listChangeTasksArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for list change tasks tool")
	}
	changeID := strings.TrimSpace(filterArgs.ChangeID)
	if changeID == "" {
		return nil, errors.New("change_id must not be empty")
	}

	st.Lock()
	defer st.Unlock()

	chg := st.Change(changeID)
	if chg == nil {
		return nil, fmt.Errorf("cannot find change with id %q", changeID)
	}

	tasks := chg.Tasks()
	items := make([]listChangeTasksItem, len(tasks))
	for i, task := range tasks {
		items[i] = taskToItem(task)
	}

	return listChangeTasksResult{ChangeID: changeID, Tasks: items}, nil
}

func (listChangeTasksTool) Validate(args map[string]any) error {
	parsedArgs, err := mcp.ToolArgsFromMap[listChangeTasksArgs](args)
	if err != nil {
		return err
	}
	return listChangeTasksTool{}.ValidateArgs(parsedArgs)
}

func (listChangeTasksTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[listChangeTasksArgs](args)
	if err != nil {
		return nil, err
	}
	return listChangeTasksTool{}.CallWithArgs(ctx, st, parsedArgs)
}

func taskToItem(task *state.Task) listChangeTasksItem {
	label, done, total := task.Progress()
	item := listChangeTasksItem{
		ID:      task.ID(),
		Kind:    task.Kind(),
		Summary: task.Summary(),
		Status:  task.Status().String(),
		Progress: struct {
			Label string `json:"label"`
			Done  int    `json:"done"`
			Total int    `json:"total"`
		}{
			Label: label,
			Done:  done,
			Total: total,
		},
	}
	if log := task.Log(); len(log) > 0 {
		item.Log = log
	}
	return item
}

func taskToMap(task *state.Task) map[string]any {
	label, done, total := task.Progress()
	result := map[string]any{
		"id":      task.ID(),
		"kind":    task.Kind(),
		"summary": task.Summary(),
		"status":  task.Status().String(),
		"progress": map[string]any{
			"label": label,
			"done":  done,
			"total": total,
		},
	}
	if log := task.Log(); len(log) > 0 {
		result["log"] = log
	}
	return result
}
