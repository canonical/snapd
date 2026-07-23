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
 */

package tasktest

import (
	"errors"
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func Snap(instanceName string) Filter {
	return func(ref TaskRef) TaskRef {
		cache := make(map[string]*snapstate.SnapSetup)
		return ref.apply(
			func(task *state.Task) (bool, error) {
				name, ok, err := snapTaskInstanceName(ref.graph, task, cache)
				if err != nil {
					return false, err
				}
				return ok && name == instanceName, nil
			},
			fmt.Sprintf("snap %q", instanceName),
		)
	}
}

func snapTaskInstanceName(graph *Graph, task *state.Task, cache map[string]*snapstate.SnapSetup) (instanceName string, ok bool, err error) {
	if task.Has("snap-setup") {
		snapsup, err := snapSetupForTask(graph, task, cache)
		if err != nil {
			return "", false, err
		}
		return snapsup.InstanceName(), !task.Has("component-setup"), nil
	}

	if task.Has("component-setup") {
		return "", false, nil
	}

	if task.Has("snap-setup-task") {
		snapsup, err := snapSetupForTask(graph, task, cache)
		if err != nil {
			return "", false, err
		}
		return snapsup.InstanceName(), !task.Has("component-setup-task"), nil
	}

	if task.Has("component-setup-task") {
		return "", false, nil
	}

	if task.Has("hook-setup") {
		var hooksup map[string]any
		if err := task.Get("hook-setup", &hooksup); err != nil {
			return "", false, err
		}

		snapName, ok := hooksup["snap"]
		if !ok {
			return "", false, errors.New("hook setup should always have a snap instance name")
		}

		instanceName, ok := snapName.(string)
		if !ok {
			return "", false, errors.New("instance name must be a string")
		}

		if component, ok := hooksup["component"]; ok {
			if _, ok := component.(string); !ok {
				return "", false, errors.New("component name must be a string")
			}
			return "", false, nil
		}

		return instanceName, true, nil
	}

	if task.Has("snapshot-setup") {
		var snapshotSetup map[string]any
		if err := task.Get("snapshot-setup", &snapshotSetup); err != nil {
			return "", false, err
		}

		instanceName, ok := snapshotSetup["snap"].(string)
		if !ok {
			return "", false, errors.New("snapshot setup should always have a snap instance name")
		}
		return instanceName, true, nil
	}

	return "", false, nil
}

func snapSetupForTask(graph *Graph, task *state.Task, cache map[string]*snapstate.SnapSetup) (*snapstate.SnapSetup, error) {
	setupTask := task
	if !task.Has("snap-setup") {
		var snapsupID string
		if err := task.Get("snap-setup-task", &snapsupID); err != nil {
			return nil, err
		}

		setupTask = graph.tasks[snapsupID]
		if setupTask == nil {
			return nil, errors.New("cannot find snap setup task")
		}
	}

	if setupTask.Has("component-setup-task") {
		return nil, errors.New("internal error: task with a snap-setup should never have a component-setup-task")
	}

	if snapsup := cache[setupTask.ID()]; snapsup != nil {
		return snapsup, nil
	}

	var snapsup snapstate.SnapSetup
	if err := setupTask.Get("snap-setup", &snapsup); err != nil {
		return nil, err
	}
	cache[setupTask.ID()] = &snapsup

	return &snapsup, nil
}
