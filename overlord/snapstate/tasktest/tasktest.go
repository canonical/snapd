package tasktest

import (
	"fmt"
	"reflect"

	"github.com/snapcore/snapd/overlord/state"
)

type TaskQuery struct {
	Kind        string
	Fields      map[string]any
	Cardinality int
}

func (q TaskQuery) Query(selection Selection) (Selection, error) {
	var matches []*state.Task
	for _, task := range selection.tasks {
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

type Selection struct {
	tasks        []*state.Task
	cache        map[any]any
	reachability map[*state.Task]map[*state.Task]bool
}

func NewSelection(tasks []*state.Task) Selection {
	tasks = append([]*state.Task(nil), tasks...)
	return Selection{
		tasks:        tasks,
		cache:        make(map[any]any),
		reachability: reachability(tasks),
	}
}

func (s Selection) subset(tasks []*state.Task) Selection {
	return Selection{
		tasks:        append([]*state.Task(nil), tasks...),
		cache:        make(map[any]any),
		reachability: s.reachability,
	}
}

func reachability(tasks []*state.Task) map[*state.Task]map[*state.Task]bool {
	table := make(map[*state.Task]map[*state.Task]bool, len(tasks))
	for _, task := range tasks {
		reachable := make(map[*state.Task]bool)
		stack := append([]*state.Task(nil), task.HaltTasks()...)
		for len(stack) != 0 {
			next := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if next == task || reachable[next] {
				continue
			}

			reachable[next] = true
			stack = append(stack, next.HaltTasks()...)
		}
		table[task] = reachable
	}
	return table
}

func (s Selection) Tasks() []*state.Task {
	return append([]*state.Task(nil), s.tasks...)
}

func (s Selection) Cached(key any) (any, bool) {
	value, ok := s.cache[key]
	return value, ok
}

func (s Selection) Cache(key, value any) {
	if value == nil {
		delete(s.cache, key)
	} else {
		s.cache[key] = value
	}
}

type Querier interface {
	Query(Selection) (Selection, error)
}

func (s Selection) Select(query Querier) Selection {
	selection, err := s.SelectErr(query)
	if err != nil {
		panic(err)
	}
	return selection
}

func (s Selection) SelectErr(query Querier) (Selection, error) {
	return query.Query(s)
}

func (s Selection) Heads() Selection {
	heads := make([]*state.Task, 0, len(s.tasks))
	for _, candidate := range s.tasks {
		head := true
		for _, other := range s.tasks {
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

func (s Selection) Tails() Selection {
	tails := make([]*state.Task, 0, len(s.tasks))
	for _, candidate := range s.tasks {
		tail := true
		for _, other := range s.tasks {
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

func (s Selection) Predecessors(of Selection) Selection {
	var predecessors []*state.Task
	for _, candidate := range s.tasks {
		for _, task := range of.tasks {
			if s.reachability[candidate][task] {
				predecessors = append(predecessors, candidate)
				break
			}
		}
	}
	return s.subset(predecessors)
}
