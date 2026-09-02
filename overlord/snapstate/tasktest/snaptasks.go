package tasktest

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

type SnapQuery struct {
	InstanceName   string
	ComponentName  string
	HookName       string
	ComponentsOnly bool
	TaskQuery      TaskQuery
}

func Snap(instanceName string) SnapQuery {
	return SnapQuery{InstanceName: instanceName}
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

	tasks := cache.snaps[q.InstanceName]
	if q.ComponentName != "" {
		tasks = cache.components[q.InstanceName][q.ComponentName]
	} else if q.ComponentsOnly {
		tasks = nil
		for _, componentTasks := range cache.components[q.InstanceName] {
			tasks = append(tasks, componentTasks...)
		}
	}

	if q.HookName != "" {
		var hookTasks []*state.Task
		for _, task := range tasks {
			if !task.Has("hook-setup") {
				continue
			}

			var hooksup hookSetup
			if err := task.Get("hook-setup", &hooksup); err != nil {
				return Selection{}, fmt.Errorf("cannot resolve hook setup for task %s (%s): %v", task.ID(), task.Kind(), err)
			}
			if hooksup.Hook == q.HookName {
				hookTasks = append(hookTasks, task)
			}
		}
		tasks = hookTasks
	}

	return q.TaskQuery.Query(selection.subset(tasks))
}

type snapQueryCacheKey struct{}

type snapQueryCache struct {
	snaps      map[string][]*state.Task
	components map[string]map[string][]*state.Task
}

func loadSnapQueryCache(selection Selection) (snapQueryCache, error) {
	if value, ok := selection.Cached(snapQueryCacheKey{}); ok {
		return value.(snapQueryCache), nil
	}

	cache, err := buildSnapQueryCache(selection)
	if err != nil {
		return snapQueryCache{}, err
	}
	selection.Cache(snapQueryCacheKey{}, cache)

	return cache, nil
}

func buildSnapQueryCache(selection Selection) (snapQueryCache, error) {
	cache := snapQueryCache{
		snaps:      make(map[string][]*state.Task),
		components: make(map[string]map[string][]*state.Task),
	}

	tasks := make(map[string]*state.Task, len(selection.tasks))
	for _, task := range selection.tasks {
		tasks[task.ID()] = task
	}

	for _, task := range selection.tasks {
		switch {
		case task.Has("hook-setup"):
			if err := cacheHookTask(cache, task); err != nil {
				return snapQueryCache{}, err
			}
		case task.Has("component-setup") || task.Has("component-setup-task"):
			if err := cacheComponentTask(cache, task, tasks); err != nil {
				return snapQueryCache{}, err
			}
		case task.Has("snap-setup") || task.Has("snap-setup-task"):
			if err := cacheSnapTask(cache, task, tasks); err != nil {
				return snapQueryCache{}, err
			}
		}
	}

	return cache, nil
}

func cacheSnapTask(cache snapQueryCache, task *state.Task, tasksByID map[string]*state.Task) error {
	snapsup, err := resolveSnapSetup(task, tasksByID)
	if err != nil {
		return fmt.Errorf("cannot resolve snap setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	instanceName := snapsup.InstanceName().String()
	cache.snaps[instanceName] = append(cache.snaps[instanceName], task)

	return nil
}

func cacheComponentTask(cache snapQueryCache, task *state.Task, tasksByID map[string]*state.Task) error {
	snapsup, err := resolveSnapSetup(task, tasksByID)
	if err != nil {
		return fmt.Errorf("cannot resolve snap setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	compsup, err := resolveComponentSetup(task, tasksByID)
	if err != nil {
		return fmt.Errorf("cannot resolve component setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	instanceName := snapsup.InstanceName().String()
	componentName := compsup.ComponentName()

	cache.snaps[instanceName] = append(cache.snaps[instanceName], task)
	components := cache.components[instanceName]
	if components == nil {
		components = make(map[string][]*state.Task)
		cache.components[instanceName] = components
	}
	components[componentName] = append(components[componentName], task)

	return nil
}

type hookSetup struct {
	Snap      string `json:"snap"`
	Hook      string `json:"hook"`
	Component string `json:"component,omitempty"`
}

func cacheHookTask(cache snapQueryCache, task *state.Task) error {
	var hooksup hookSetup
	if err := task.Get("hook-setup", &hooksup); err != nil {
		return fmt.Errorf("cannot resolve hook setup for task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	cache.snaps[hooksup.Snap] = append(cache.snaps[hooksup.Snap], task)
	if hooksup.Component != "" {
		components := cache.components[hooksup.Snap]
		if components == nil {
			components = make(map[string][]*state.Task)
			cache.components[hooksup.Snap] = components
		}
		components[hooksup.Component] = append(components[hooksup.Component], task)
	}

	return nil
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
