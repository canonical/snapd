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

package tasktest

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// SnapQuery extends TaskQuery with criteria for selecting tasks associated with
// snaps, components, and hooks.
//
// SnapQuery provides builder-style methods that return more constrained copies
// of itself. These enable this kind of pattern:
//
//	app := tasktest.Snap("snap-name")
//	linkApp := app.WithKind("link-snap")
//	mountApp := app.WithKind("mount-snap")
type SnapQuery struct {
	SnapTaskAttributes
	TaskQuery TaskQuery

	// ComponentsOnly, when true, limits the query to tasks associated with a
	// component.
	ComponentsOnly bool
}

// SnapTaskAttributes identifies the snap, component, and hook associated with a
// task. When embedded in SnapQuery, non-empty fields are used as matching
// criteria.
type SnapTaskAttributes struct {
	// InstanceName is the instance name of the snap associated with the task.
	InstanceName string
	// ComponentName is the component associated with the task. It is empty for
	// tasks that are not associated with a component.
	ComponentName string
	// HookName is the hook associated with the task. It is empty for tasks that
	// do not run a hook.
	HookName string
}

// Snap is the entry point for building a SnapQuery. The resulting query matches
// tasks associated with the given instance name and expects exactly one match
// unless TaskCount or All is used.
func Snap(instanceName string) SnapQuery {
	return SnapQuery{
		SnapTaskAttributes: SnapTaskAttributes{InstanceName: instanceName},
	}
}

// WithKind limits the query to tasks that are of the given kind.
func (q SnapQuery) WithKind(kind string) SnapQuery {
	q.TaskQuery.Kind = kind
	return q
}

// WithField limits the query to tasks whose given field matches the expected
// value. A nil value only matches tasks that do not have the field.
func (q SnapQuery) WithField(field string, expected any) SnapQuery {
	q.TaskQuery = q.TaskQuery.WithField(field, expected)
	return q
}

// WithComponent limits the query to tasks that are associated with the given
// component name.
func (q SnapQuery) WithComponent(componentName string) SnapQuery {
	q.ComponentName = componentName
	return q
}

// Components limits the query to tasks that are associated with some component.
func (q SnapQuery) Components() SnapQuery {
	q.ComponentsOnly = true
	return q
}

// WithHook limits the query to tasks that are associated with the given hook name.
func (q SnapQuery) WithHook(hookName string) SnapQuery {
	q.HookName = hookName
	q.TaskQuery.Kind = "run-hook"
	return q
}

// TaskCount enforces that this query expects the given number of tasks in a
// resulting Selection.
func (q SnapQuery) TaskCount(count int) SnapQuery {
	q.TaskQuery = q.TaskQuery.TaskCount(count)
	return q
}

// All enforces that this query expects any non-zero number of tasks in a
// resulting Selection.
func (q SnapQuery) All() SnapQuery {
	q.TaskQuery = q.TaskQuery.All()
	return q
}

// Query returns a subset of the given Selection that match against the fields
// set in this SnapQuery.
func (q SnapQuery) Query(selection Selection) (Selection, error) {
	cache, err := loadSnapQueryCache(selection)
	if err != nil {
		return Selection{}, err
	}

	matches, err := selection.Filter(func(task *state.Task) (bool, error) {
		info, ok := cache[task.ID()]
		if !ok || info.InstanceName != q.InstanceName {
			return false, nil
		}
		if q.ComponentName != "" && info.ComponentName != q.ComponentName {
			return false, nil
		}
		if q.ComponentsOnly && info.ComponentName == "" {
			return false, nil
		}
		if q.HookName != "" && info.HookName != q.HookName {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return Selection{}, err
	}

	return q.TaskQuery.Query(matches)
}

type snapQueryCacheKey struct{}

func loadSnapQueryCache(selection Selection) (map[string]SnapTaskAttributes, error) {
	if value, ok := selection.cache[snapQueryCacheKey{}]; ok {
		return value.(map[string]SnapTaskAttributes), nil
	}

	cache, err := buildSnapQueryCache(selection.universe)
	if err != nil {
		return nil, err
	}
	selection.cache[snapQueryCacheKey{}] = cache

	return cache, nil
}

func buildSnapQueryCache(universe []*state.Task) (map[string]SnapTaskAttributes, error) {
	cache := make(map[string]SnapTaskAttributes, len(universe))

	tasks := make(map[string]*state.Task, len(universe))
	for _, task := range universe {
		tasks[task.ID()] = task
	}

	for _, task := range universe {
		var info SnapTaskAttributes
		var err error
		switch {
		case task.Has("hook-setup"):
			info, err = snapTaskAttributesForHook(task)
		case task.Has("component-setup") || task.Has("component-setup-task"):
			info, err = snapTaskAttributesForComponent(task, tasks)
		case task.Has("snap-setup") || task.Has("snap-setup-task"):
			info, err = snapTaskAttributesForSnap(task, tasks)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		cache[task.ID()] = info
	}

	return cache, nil
}

func snapTaskAttributesForSnap(task *state.Task, tasks map[string]*state.Task) (SnapTaskAttributes, error) {
	snapsup, err := resolveSnapSetup(task, tasks)
	if err != nil {
		return SnapTaskAttributes{}, fmt.Errorf("cannot resolve snap setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	return SnapTaskAttributes{InstanceName: snapsup.InstanceName().String()}, nil
}

func snapTaskAttributesForComponent(task *state.Task, tasksByID map[string]*state.Task) (SnapTaskAttributes, error) {
	snapsup, err := resolveSnapSetup(task, tasksByID)
	if err != nil {
		return SnapTaskAttributes{}, fmt.Errorf("cannot resolve snap setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	compsup, err := resolveComponentSetup(task, tasksByID)
	if err != nil {
		return SnapTaskAttributes{}, fmt.Errorf("cannot resolve component setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	return SnapTaskAttributes{
		InstanceName:  snapsup.InstanceName().String(),
		ComponentName: compsup.ComponentName(),
	}, nil
}

type hookSetup struct {
	Snap      string `json:"snap"`
	Hook      string `json:"hook"`
	Component string `json:"component,omitempty"`
}

func snapTaskAttributesForHook(task *state.Task) (SnapTaskAttributes, error) {
	var hooksup hookSetup
	if err := task.Get("hook-setup", &hooksup); err != nil {
		return SnapTaskAttributes{}, fmt.Errorf("cannot resolve hook setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	return SnapTaskAttributes{
		InstanceName:  hooksup.Snap,
		ComponentName: hooksup.Component,
		HookName:      hooksup.Hook,
	}, nil
}

func resolveSnapSetup(task *state.Task, tasks map[string]*state.Task) (snapstate.SnapSetup, error) {
	if task.Has("snap-setup") {
		var snapsup snapstate.SnapSetup
		if err := task.Get("snap-setup", &snapsup); err != nil {
			return snapstate.SnapSetup{}, err
		}
		return snapsup, nil
	}

	var snapsupID string
	if err := task.Get("snap-setup-task", &snapsupID); err != nil {
		return snapstate.SnapSetup{}, err
	}

	t := tasks[snapsupID]
	if t == nil {
		return snapstate.SnapSetup{}, fmt.Errorf("cannot find snap setup task %q", snapsupID)
	}

	var snapsup snapstate.SnapSetup
	if err := t.Get("snap-setup", &snapsup); err != nil {
		return snapstate.SnapSetup{}, err
	}
	return snapsup, nil
}

func resolveComponentSetup(task *state.Task, tasks map[string]*state.Task) (snapstate.ComponentSetup, error) {
	if task.Has("component-setup") {
		var compsup snapstate.ComponentSetup
		if err := task.Get("component-setup", &compsup); err != nil {
			return snapstate.ComponentSetup{}, err
		}
		return compsup, nil
	}

	var compsupID string
	if err := task.Get("component-setup-task", &compsupID); err != nil {
		return snapstate.ComponentSetup{}, err
	}

	t := tasks[compsupID]
	if t == nil {
		return snapstate.ComponentSetup{}, fmt.Errorf("cannot find component setup task %q", compsupID)
	}

	var compsup snapstate.ComponentSetup
	if err := t.Get("component-setup", &compsup); err != nil {
		return snapstate.ComponentSetup{}, err
	}
	return compsup, nil
}
