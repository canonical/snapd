package tasktest

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

type SnapQuery struct {
	SnapTaskInfo
	ComponentsOnly bool
	TaskQuery      TaskQuery
}

type SnapTaskInfo struct {
	InstanceName  string
	ComponentName string
	HookName      string
}

func Snap(instanceName string) SnapQuery {
	return SnapQuery{
		SnapTaskInfo: SnapTaskInfo{InstanceName: instanceName},
	}
}

func (q SnapQuery) WithKind(kind string) SnapQuery {
	q.TaskQuery.Kind = kind
	return q
}

func (q SnapQuery) WithField(field string, expected any) SnapQuery {
	fields := make(map[string]any, len(q.TaskQuery.Fields)+1)
	for f, value := range q.TaskQuery.Fields {
		fields[f] = value
	}
	fields[field] = expected
	q.TaskQuery.Fields = fields

	return q
}

func (q SnapQuery) WithComponent(componentName string) SnapQuery {
	q.ComponentName = componentName
	return q
}

func (q SnapQuery) Components() SnapQuery {
	q.ComponentsOnly = true
	return q
}

func (q SnapQuery) WithHook(hookName string) SnapQuery {
	q.HookName = hookName
	q.TaskQuery.Kind = "run-hook"
	return q
}

func (q SnapQuery) TaskCount(count int) SnapQuery {
	q.TaskQuery.Cardinality = count
	return q
}

func (q SnapQuery) All() SnapQuery {
	q.TaskQuery.Cardinality = -1
	return q
}

func (q SnapQuery) Query(selection Selection) (Selection, error) {
	cache, err := loadSnapQueryCache(selection)
	if err != nil {
		return Selection{}, err
	}

	var matches []*state.Task
	for _, task := range selection.selected {
		info, ok := cache[task.ID()]
		if !ok || info.InstanceName != q.InstanceName {
			continue
		}
		if q.ComponentName != "" && info.ComponentName != q.ComponentName {
			continue
		}
		if q.ComponentsOnly && info.ComponentName == "" {
			continue
		}
		if q.HookName != "" && info.HookName != q.HookName {
			continue
		}
		matches = append(matches, task)
	}

	return q.TaskQuery.Query(selection.subset(matches))
}

type snapQueryCacheKey struct{}

func loadSnapQueryCache(selection Selection) (map[string]SnapTaskInfo, error) {
	if value, ok := selection.Cached(snapQueryCacheKey{}); ok {
		return value.(map[string]SnapTaskInfo), nil
	}

	cache, err := buildSnapQueryCache(selection.universe)
	if err != nil {
		return nil, err
	}
	selection.Cache(snapQueryCacheKey{}, cache)

	return cache, nil
}

func buildSnapQueryCache(universe []*state.Task) (map[string]SnapTaskInfo, error) {
	cache := make(map[string]SnapTaskInfo, len(universe))

	tasks := make(map[string]*state.Task, len(universe))
	for _, task := range universe {
		tasks[task.ID()] = task
	}

	for _, task := range universe {
		var info SnapTaskInfo
		var err error
		switch {
		case task.Has("hook-setup"):
			info, err = snapTaskInfoForHook(task)
		case task.Has("component-setup") || task.Has("component-setup-task"):
			info, err = snapTaskInfoForComponent(task, tasks)
		case task.Has("snap-setup") || task.Has("snap-setup-task"):
			info, err = snapTaskInfoForSnap(task, tasks)
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

func snapTaskInfoForSnap(task *state.Task, tasks map[string]*state.Task) (SnapTaskInfo, error) {
	snapsup, err := resolveSnapSetup(task, tasks)
	if err != nil {
		return SnapTaskInfo{}, fmt.Errorf("cannot resolve snap setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	return SnapTaskInfo{InstanceName: snapsup.InstanceName().String()}, nil
}

func snapTaskInfoForComponent(task *state.Task, tasksByID map[string]*state.Task) (SnapTaskInfo, error) {
	snapsup, err := resolveSnapSetup(task, tasksByID)
	if err != nil {
		return SnapTaskInfo{}, fmt.Errorf("cannot resolve snap setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	compsup, err := resolveComponentSetup(task, tasksByID)
	if err != nil {
		return SnapTaskInfo{}, fmt.Errorf("cannot resolve component setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	return SnapTaskInfo{
		InstanceName:  snapsup.InstanceName().String(),
		ComponentName: compsup.ComponentName(),
	}, nil
}

type hookSetup struct {
	Snap      string `json:"snap"`
	Hook      string `json:"hook"`
	Component string `json:"component,omitempty"`
}

func snapTaskInfoForHook(task *state.Task) (SnapTaskInfo, error) {
	var hooksup hookSetup
	if err := task.Get("hook-setup", &hooksup); err != nil {
		return SnapTaskInfo{}, fmt.Errorf("cannot resolve hook setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	return SnapTaskInfo{
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
