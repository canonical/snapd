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

	"github.com/snapcore/snapd/overlord/state"
)

type Assertion func(*Graph) error

func (graph *Graph) Assert(assertions ...Assertion) error {
	for _, assertion := range assertions {
		if err := assertion(graph); err != nil {
			return err
		}
	}
	return nil
}

func Resolves(refs ...TaskRef) Assertion {
	return func(graph *Graph) error {
		if _, err := graph.resolve(refs...); err != nil {
			return err
		}
		return nil
	}
}

func Ordered(refs ...TaskRef) Assertion {
	return func(graph *Graph) error {
		tasks, err := graph.resolve(refs...)
		if err != nil {
			return err
		}

		return checkOrdered(graph, tasks)
	}
}

func checkOrdered(graph *Graph, groups [][]*state.Task) error {
	if len(groups) < 2 {
		return errors.New("ordered requires at least two task references")
	}

	for i := 0; i < len(groups)-1; i++ {
		for _, first := range groups[i] {
			reachable := graph.reachability[first.ID()]
			for _, second := range groups[i+1] {
				if !reachable[second.ID()] {
					return fmt.Errorf("task %s (%s) does not precede task %s (%s)", first.ID(), first.Kind(), second.ID(), second.Kind())
				}
			}
		}
	}
	return nil
}

// Unordered checks that no task transitively precedes another task.
func Unordered(refs ...TaskRef) Assertion {
	return func(graph *Graph) error {
		groups, err := graph.resolve(refs...)
		if err != nil {
			return err
		}

		return checkUnordered(graph, groups)
	}
}

func checkUnordered(graph *Graph, groups [][]*state.Task) error {
	if len(groups) < 2 {
		return errors.New("unordered requires at least two task references")
	}

	for i, group := range groups {
		for _, other := range groups[i+1:] {
			for _, first := range group {
				for _, second := range other {
					if first == second {
						return fmt.Errorf("task %s (%s) cannot be compared against itself", first.ID(), first.Kind())
					}

					if graph.reachability[first.ID()][second.ID()] || graph.reachability[second.ID()][first.ID()] {
						return fmt.Errorf("tasks %s (%s) and %s (%s) are ordered", first.ID(), first.Kind(), second.ID(), second.Kind())
					}
				}
			}
		}
	}
	return nil
}

// LanesSubset checks that the lane sets of all tasks in subset are contained
// by the lane set of every task in superset.
func LanesSubset(subset, superset TaskRef) Assertion {
	return func(graph *Graph) error {
		groups, err := graph.resolve(subset, superset)
		if err != nil {
			return err
		}

		return checkLanesSubset(groups[0], groups[1])
	}
}

func checkLanesSubset(subset, superset []*state.Task) error {
	required := make(map[int]*state.Task)
	for _, task := range subset {
		for _, lane := range task.Lanes() {
			if _, ok := required[lane]; !ok {
				required[lane] = task
			}
		}
	}

	for _, super := range superset {
		lanes := set(super.Lanes())
		for lane, sub := range required {
			if !lanes[lane] {
				return fmt.Errorf(
					"lane set of task %s (%s) does not contain lane %d from task %s (%s)",
					super.ID(),
					super.Kind(),
					lane,
					sub.ID(),
					sub.Kind(),
				)
			}
		}
	}
	return nil
}

// LanesDisjoint checks that tasks in different references do not share lanes.
func LanesDisjoint(refs ...TaskRef) Assertion {
	return func(graph *Graph) error {
		groups, err := graph.resolve(refs...)
		if err != nil {
			return err
		}

		return checkLanesDisjoint(groups)
	}
}

func checkLanesDisjoint(groups [][]*state.Task) error {
	if len(groups) < 2 {
		return errors.New("lanes disjoint requires at least two task references")
	}

	seen := make(map[int]*state.Task)
	for _, group := range groups {
		incoming := make(map[int]*state.Task)
		for _, task := range group {
			for _, lane := range task.Lanes() {
				if prev := seen[lane]; prev != nil {
					return fmt.Errorf("tasks %s (%s) and %s (%s) share lane %d",
						prev.ID(),
						prev.Kind(),
						task.ID(),
						task.Kind(),
						lane,
					)
				}
				if incoming[lane] == nil {
					incoming[lane] = task
				}
			}
		}

		for lane, task := range incoming {
			seen[lane] = task
		}
	}
	return nil
}

// LanesEqual checks that tasks in different references have the same lane
// set.
func LanesEqual(refs ...TaskRef) Assertion {
	return func(graph *Graph) error {
		groups, err := graph.resolve(refs...)
		if err != nil {
			return err
		}

		return checkLaneSetsEqual(groups)
	}
}

func checkLaneSetsEqual(groups [][]*state.Task) error {
	if len(groups) < 2 {
		return errors.New("lane sets equal requires at least two task references")
	}

	var all []*state.Task
	for _, group := range groups {
		all = append(all, group...)
	}

	first := all[0]
	expected := set(first.Lanes())
	for _, second := range all[1:] {
		got := set(second.Lanes())

		same := len(expected) == len(got)
		for lane := range expected {
			if !got[lane] {
				same = false
				break
			}
		}

		if !same {
			return fmt.Errorf("tasks %s (%s) and %s (%s) have different lane sets: %v != %v",
				first.ID(),
				first.Kind(),
				second.ID(),
				second.Kind(),
				first.Lanes(),
				second.Lanes(),
			)
		}
	}

	return nil
}

func set[T comparable](s []T) map[T]bool {
	m := make(map[T]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}
