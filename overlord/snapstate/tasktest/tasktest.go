package tasktest

import (
	"fmt"
	"reflect"

	"github.com/snapcore/snapd/overlord/state"
)

// Selection represents a fixed selection set of tasks. A root Selection will be
// created by a caller with an initial set of tasks. Each child Selection is a
// subset of that root Selection.
type Selection struct {
	// selected is the set of tasks currently represented by this Selection.
	selected []*state.Task

	// universe is the full set of tasks from which the root Selection was
	// created.
	universe []*state.Task
	// cache carries universe-derived data shared by the root Selection and all
	// of its subsets. Cached data must not depend on the currently selected
	// tasks.
	cache map[any]any
	// reachability maps each task to the tasks that transitively depend on it.
	reachability map[*state.Task]map[*state.Task]bool
}

// NewSelection creates a new root Selection.
func NewSelection(tasks []*state.Task) Selection {
	tasks = append([]*state.Task(nil), tasks...)
	return Selection{
		selected:     tasks,
		universe:     tasks,
		cache:        make(map[any]any),
		reachability: reachability(tasks),
	}
}

// Filter returns a new Selection containing the tasks for which predicate
// returns true. The new Selection shares the same universe, cache, and
// reachability table as the original Selection.
func (s Selection) Filter(predicate func(*state.Task) (bool, error)) (Selection, error) {
	selected := make([]*state.Task, 0, len(s.selected))
	for _, task := range s.selected {
		matches, err := predicate(task)
		if err != nil {
			return Selection{}, err
		}
		if matches {
			selected = append(selected, task)
		}
	}

	return Selection{
		selected:     selected,
		universe:     s.universe,
		cache:        s.cache,
		reachability: s.reachability,
	}, nil
}

// reachability returns a mapping of each task to the tasks that transitively
// depend on it.
func reachability(tasks []*state.Task) map[*state.Task]map[*state.Task]bool {
	table := make(map[*state.Task]map[*state.Task]bool, len(tasks))
	for _, task := range tasks {
		reachable := make(map[*state.Task]bool)
		stack := append([]*state.Task(nil), task.HaltTasks()...)
		for len(stack) != 0 {
			next := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if next == task {
				panic(fmt.Sprintf("dependency cycle involving task %s (%s)", task.ID(), task.Kind()))
			}
			if reachable[next] {
				continue
			}

			reachable[next] = true
			stack = append(stack, next.HaltTasks()...)
		}
		table[task] = reachable
	}
	return table
}

// Tasks returns the tasks that this Selection represents.
func (s Selection) Tasks() []*state.Task {
	return append([]*state.Task(nil), s.selected...)
}

// Querier is the interface that enables a type to narrow a Selection.
type Querier interface {
	// Query returns a Selection that is a subset of the given Selection. The
	// concrete type that implements Querier should carry data that is used to
	// filter the tasks in the given Selection.
	Query(Selection) (Selection, error)
}

// Select wraps Selection.SelectErr and panics if an error is returned.
func (s Selection) Select(query Querier) Selection {
	selection, err := s.SelectErr(query)
	if err != nil {
		panic(err)
	}
	return selection
}

// SelectErr applies a Querier to this Selection.
func (s Selection) SelectErr(query Querier) (Selection, error) {
	return query.Query(s)
}

// Heads returns the subset of tasks in this Selection that are not blocked by
// any other tasks in this Selection.
func (s Selection) Heads() Selection {
	heads, err := s.Filter(func(candidate *state.Task) (bool, error) {
		for _, other := range s.selected {
			if s.reachability[other][candidate] {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		panic(fmt.Sprintf("infallible call failed: %v", err))
	}
	return heads
}

// Tails returns the subset of tasks in this Selection that do not block any
// other tasks in this Selection.
func (s Selection) Tails() Selection {
	tails, err := s.Filter(func(candidate *state.Task) (bool, error) {
		for _, other := range s.selected {
			if s.reachability[candidate][other] {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		panic(fmt.Sprintf("infallible call failed: %v", err))
	}
	return tails
}

// Predecessors returns the subset of tasks in this Selection that block at
// least one task in the given Selection.
func (s Selection) Predecessors(of Selection) Selection {
	predecessors, err := s.Filter(func(candidate *state.Task) (bool, error) {
		for _, task := range of.selected {
			if s.reachability[candidate][task] {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		panic(fmt.Sprintf("infallible call failed: %v", err))
	}
	return predecessors
}

// TaskQuery is an implementation of Querier that enables selecting a set of tasks
// based on generic properties of the task itself.
type TaskQuery struct {
	// Kind is the kind of tasks that are matched by this query.
	Kind string
	// Fields contains the fields that must be carried by tasks that match this
	// query. Explicitly nil fields must be absent.
	Fields map[string]any
	// Cardinality defines how many tasks this query should match. A cardinality
	// of zero indicates that exactly one task should be matched. A cardinality
	// of -1 indicates that any non-zero number of tasks should be matched.
	Cardinality int
}

// Kind creates a TaskQuery that matches tasks of the given kind.
func Kind(kind string) TaskQuery {
	return TaskQuery{Kind: kind}
}

// TaskCount returns a query that expects the given number of matching tasks.
func (q TaskQuery) TaskCount(count int) TaskQuery {
	q.Cardinality = count
	return q
}

// All returns a query that accepts any non-zero number of matching tasks.
func (q TaskQuery) All() TaskQuery {
	q.Cardinality = -1
	return q
}

// Query returns a subset of the tasks in the given Selection that match this
// TaskQuery.
func (q TaskQuery) Query(selection Selection) (Selection, error) {
	if q.Cardinality < -1 {
		return Selection{}, fmt.Errorf("invalid task query cardinality %d", q.Cardinality)
	}

	matches, err := selection.Filter(func(task *state.Task) (bool, error) {
		if q.Kind != "" && task.Kind() != q.Kind {
			return false, nil
		}

		for field, expected := range q.Fields {
			if expected == nil {
				if task.Has(field) {
					return false, nil
				}
				continue
			}

			if !task.Has(field) {
				return false, nil
			}

			expectedType := reflect.TypeOf(expected)
			actual := reflect.New(expectedType)
			if err := task.Get(field, actual.Interface()); err != nil {
				return false, fmt.Errorf("cannot read field %q of task %s (%s) as %v", field, task.ID(), task.Kind(), expectedType)
			}

			if !reflect.DeepEqual(actual.Elem().Interface(), expected) {
				return false, nil
			}
		}

		return true, nil
	})
	if err != nil {
		return Selection{}, err
	}

	if len(matches.selected) == 0 {
		return Selection{}, fmt.Errorf("task query matched no tasks")
	}

	// by default, we treat an unset cardinality as expecting exactly one task.
	// if we need to query for the absence of tasks, we'll rework this.
	cardinality := q.Cardinality
	if cardinality == 0 {
		cardinality = 1
	}

	// cardinality of -1 implies any non-zero number of tasks is expected
	if cardinality != -1 && len(matches.selected) != cardinality {
		return Selection{}, fmt.Errorf("task query matched %d tasks, expected %d", len(matches.selected), cardinality)
	}

	return matches, nil
}
