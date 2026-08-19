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

// Package tasktest provides helpers for asserting properties of task graphs.
package tasktest

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/snapcore/snapd/overlord/state"
)

type TaskRef struct {
	graph *Graph

	filtered []*state.Task
	queries  []string
	err      error
	multiple bool
}

type Filter func(TaskRef) TaskRef

type Graph struct {
	TaskRef

	tasks        map[string]*state.Task
	reachability map[string]map[string]bool
}

func NewGraph(tasks []*state.Task) (*Graph, error) {
	tasks = append([]*state.Task(nil), tasks...)
	reachability, err := reachabilityTable(tasks)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*state.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID()] = task
	}

	graph := &Graph{
		tasks:        byID,
		reachability: reachability,
	}

	graph.TaskRef = TaskRef{
		graph:    graph,
		filtered: tasks,
		queries:  []string{"all tasks"},
	}

	return graph, nil
}

func (ref TaskRef) apply(predicate func(*state.Task) (bool, error), query string) TaskRef {
	if ref.terminal() {
		return ref
	}

	queries := make([]string, len(ref.queries)+1)
	copy(queries, ref.queries)
	queries[len(ref.queries)] = query

	var tasks []*state.Task
	for _, task := range ref.filtered {
		matches, err := predicate(task)
		if err != nil {
			ref.filtered = nil
			ref.queries = queries
			ref.err = fmt.Errorf("%s failed: %w", strings.Join(queries, " -> "), err)
			return ref
		}
		if matches {
			tasks = append(tasks, task)
		}
	}

	if len(tasks) == 0 {
		ref.filtered = nil
		ref.queries = queries
		return ref
	}

	ref.filtered = tasks
	ref.queries = queries
	return ref
}

func (ref TaskRef) terminal() bool {
	return ref.err != nil || len(ref.filtered) == 0
}

func (ref TaskRef) Where(filter Filter) TaskRef {
	return filter(ref)
}

func (ref TaskRef) Kind(kind string) TaskRef {
	return ref.apply(
		func(task *state.Task) (bool, error) {
			return task.Kind() == kind, nil
		},
		fmt.Sprintf("kind %q", kind),
	)
}

func (ref TaskRef) With(key string) TaskRef {
	return ref.apply(
		func(task *state.Task) (bool, error) {
			return task.Has(key), nil
		},
		fmt.Sprintf("with %q", key),
	)
}

func (ref TaskRef) Without(key string) TaskRef {
	return ref.apply(
		func(task *state.Task) (bool, error) {
			return !task.Has(key), nil
		},
		fmt.Sprintf("without %q", key),
	)
}

func (ref TaskRef) Has(key string, value any) TaskRef {
	return ref.apply(
		func(task *state.Task) (bool, error) {
			if !task.Has(key) {
				return false, nil
			}

			valueType := reflect.TypeOf(value)
			if valueType == nil {
				return false, errors.New("value cannot be nil")
			}

			actual := reflect.New(valueType)
			if err := task.Get(key, actual.Interface()); err != nil {
				return false, err
			}

			return reflect.DeepEqual(actual.Elem().Interface(), value), nil
		},
		fmt.Sprintf("has %q value %#v", key, value),
	)
}

func (ref TaskRef) Before(boundary TaskRef) TaskRef {
	var boundaries []*state.Task
	return ref.apply(
		func(task *state.Task) (bool, error) {
			if len(boundaries) == 0 {
				if ref.graph != boundary.graph {
					return false, errors.New("task references belong to different graphs")
				}

				ts, err := boundary.ResolveMany()
				if err != nil {
					return false, err
				}

				boundaries = ts
			}

			for _, bt := range boundaries {
				if !ref.graph.reachability[task.ID()][bt.ID()] {
					return false, nil
				}
			}
			return true, nil
		},
		fmt.Sprintf("before (%s)", strings.Join(boundary.queries, " -> ")),
	)
}

func (ref TaskRef) First() TaskRef {
	return ref.apply(
		func(candidate *state.Task) (bool, error) {
			for _, other := range ref.filtered {
				if ref.graph.reachability[other.ID()][candidate.ID()] {
					return false, nil
				}
			}
			return true, nil
		},
		"first",
	)
}

func (ref TaskRef) Last() TaskRef {
	return ref.apply(
		func(candidate *state.Task) (bool, error) {
			for _, other := range ref.filtered {
				if ref.graph.reachability[candidate.ID()][other.ID()] {
					return false, nil
				}
			}
			return true, nil
		},
		"last",
	)
}

func (ref TaskRef) All() TaskRef {
	ref.multiple = true
	return ref
}

func (ref TaskRef) ResolveMany() ([]*state.Task, error) {
	if ref.err != nil {
		return nil, ref.err
	}

	query := strings.Join(ref.queries, " -> ")
	switch len(ref.filtered) {
	case 0:
		return nil, fmt.Errorf("%s matched no tasks", query)
	case 1:
		return ref.filtered, nil
	default:
		if ref.multiple {
			return ref.filtered, nil
		}

		descriptions := make([]string, len(ref.filtered))
		for i, task := range ref.filtered {
			descriptions[i] = fmt.Sprintf("%s (%s)", task.ID(), task.Kind())
		}
		sort.Strings(descriptions)
		return nil, fmt.Errorf("%s matched multiple tasks: %s", query, strings.Join(descriptions, ", "))
	}
}

func (ref TaskRef) Resolve() (*state.Task, error) {
	ref.multiple = false
	tasks, err := ref.ResolveMany()
	if err != nil {
		return nil, err
	}
	return tasks[0], nil
}

func (graph *Graph) resolve(refs ...TaskRef) ([][]*state.Task, error) {
	tasks := make([][]*state.Task, len(refs))
	for i, ref := range refs {
		if ref.graph != graph {
			return nil, errors.New("task references belong to different graphs")
		}

		resolved, err := ref.ResolveMany()
		if err != nil {
			return nil, fmt.Errorf("cannot resolve task reference: %w", err)
		}
		tasks[i] = resolved
	}
	return tasks, nil
}

func reachabilityTable(tasks []*state.Task) (map[string]map[string]bool, error) {
	ordered, err := topologicalSort(tasks)
	if err != nil {
		return nil, err
	}

	table := make(map[string]map[string]bool, len(tasks))
	for i := len(ordered) - 1; i >= 0; i-- {
		t := ordered[i]
		reachable := make(map[string]bool)

		for _, successor := range t.HaltTasks() {
			reachable[successor.ID()] = true
			for id := range table[successor.ID()] {
				reachable[id] = true
			}
		}

		table[t.ID()] = reachable
	}

	return table, nil
}

func topologicalSort(tasks []*state.Task) ([]*state.Task, error) {
	// create lookup map so that we can make sure that the input tasks are
	// unique and the graph is self-contained
	lookup := make(map[string]*state.Task, len(tasks))

	edges := make(map[string]int, len(tasks))
	ordered := make([]*state.Task, 0, len(tasks))

	// collect each task's initial edge count and create set of initial graph
	// entry points to visit
	for _, t := range tasks {
		if _, ok := lookup[t.ID()]; ok {
			return nil, errors.New("tasks must be unique")
		}

		lookup[t.ID()] = t

		wait := t.WaitTasks()
		edges[t.ID()] = len(wait)
		if len(wait) == 0 {
			ordered = append(ordered, t)
		}
	}

	// make sure that all tasks only reference other tasks in the input
	for _, t := range tasks {
		for _, w := range t.WaitTasks() {
			if _, ok := lookup[w.ID()]; !ok {
				return nil, errors.New("input task waits on external task")
			}
		}

		for _, h := range t.HaltTasks() {
			if _, ok := lookup[h.ID()]; !ok {
				return nil, errors.New("external task waits on input task")
			}
		}
	}

	for i := 0; i < len(ordered); i++ {
		t := ordered[i]
		for _, h := range t.HaltTasks() {
			edges[h.ID()]--
			if edges[h.ID()] == 0 {
				ordered = append(ordered, h)
			}
		}
	}

	if len(tasks) != len(ordered) {
		return nil, errors.New("cyclic graph detected")
	}

	return ordered, nil
}
