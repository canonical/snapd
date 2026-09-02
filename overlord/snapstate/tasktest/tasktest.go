package tasktest

import (
	"fmt"
	"reflect"

	"github.com/snapcore/snapd/overlord/state"
)

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

// Query returns a subset of the tasks in the given Selection that match this
// TaskQuery.
func (q TaskQuery) Query(selection Selection) (Selection, error) {
	var matches []*state.Task
	for _, task := range selection.selected {
		if q.Kind != "" && task.Kind() != q.Kind {
			continue
		}

		matched := true
		for field, expected := range q.Fields {
			if expected == nil {
				if task.Has(field) {
					matched = false
					break
				}
				continue
			}

			if !task.Has(field) {
				matched = false
				break
			}

			actual := reflect.New(reflect.TypeOf(expected))
			if err := task.Get(field, actual.Interface()); err != nil {
				return Selection{}, fmt.Errorf("cannot query task %s field %q: %v", task.ID(), field, err)
			}
			if !reflect.DeepEqual(actual.Elem().Interface(), expected) {
				matched = false
				break
			}
		}

		if matched {
			matches = append(matches, task)
		}
	}

	cardinality := q.Cardinality
	if cardinality == 0 {
		cardinality = 1
	}
	switch {
	case cardinality == -1:
		if len(matches) == 0 {
			return Selection{}, fmt.Errorf("task query matched no tasks")
		}
	case cardinality < -1:
		return Selection{}, fmt.Errorf("invalid task query cardinality %d", cardinality)
	case len(matches) != cardinality:
		return Selection{}, fmt.Errorf("task query matched %d tasks, expected %d", len(matches), cardinality)
	}

	return selection.subset(matches), nil
}

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

// subset returns a new Selection that shares the same universe, cache, and
// reachability table as the parent Selection. The given tasks must be a subset
// of the current Selection.
func (s Selection) subset(tasks []*state.Task) Selection {
	return Selection{
		selected:     append([]*state.Task(nil), tasks...),
		universe:     s.universe,
		cache:        s.cache,
		reachability: s.reachability,
	}
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

// Cached returns an arbitrary value that has been cached with this Selection.
// Should be used by Querier implementations.
func (s Selection) Cached(key any) (any, bool) {
	value, ok := s.cache[key]
	return value, ok
}

// Cache stores an arbitrary value in the cache of this Selection. Should be
// used by Querier implementations.
func (s Selection) Cache(key, value any) {
	if value == nil {
		delete(s.cache, key)
	} else {
		s.cache[key] = value
	}
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
	heads := make([]*state.Task, 0, len(s.selected))
	for _, candidate := range s.selected {
		head := true
		for _, other := range s.selected {
			if s.reachability[other][candidate] {
				head = false
				break
			}
		}
		if head {
			heads = append(heads, candidate)
		}
	}
	return s.subset(heads)
}

// Tails returns the subset of tasks in this Selection that do not block any
// other tasks in this Selection.
func (s Selection) Tails() Selection {
	tails := make([]*state.Task, 0, len(s.selected))
	for _, candidate := range s.selected {
		tail := true
		for _, other := range s.selected {
			if s.reachability[candidate][other] {
				tail = false
				break
			}
		}
		if tail {
			tails = append(tails, candidate)
		}
	}
	return s.subset(tails)
}

// Predecessors returns the subset of tasks in this Selection that block at
// least one task in the given Selection.
func (s Selection) Predecessors(of Selection) Selection {
	var predecessors []*state.Task
	for _, candidate := range s.selected {
		for _, task := range of.selected {
			if s.reachability[candidate][task] {
				predecessors = append(predecessors, candidate)
				break
			}
		}
	}
	return s.subset(predecessors)
}
