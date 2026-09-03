package tasktest

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/state"
)

// AssertSequenced checks that the given Selections form a sequence in the task
// graph. Every task in each Selection must transitively precede every task in
// the following Selection. Task ordering within a Selection is not considered.
func AssertSequenced(selections ...Selection) error {
	if err := validateNonEmpty(selections...); err != nil {
		return err
	}

	for i := 0; i+1 < len(selections); i++ {
		before := selections[i]
		after := selections[i+1]
		for _, beforeTask := range before.selected {
			for _, afterTask := range after.selected {
				if !before.reachability[beforeTask][afterTask] {
					return fmt.Errorf("task %s (%s) is not sequenced before task %s (%s)", beforeTask.ID(), beforeTask.Kind(), afterTask.ID(), afterTask.Kind())
				}
			}
		}
	}
	return nil
}

// AssertNotSequenced checks that no task in first transitively precedes any
// task in second.
func AssertNotSequenced(first, second Selection) error {
	if err := validateNonEmpty(first, second); err != nil {
		return err
	}

	for _, firstTask := range first.selected {
		for _, secondTask := range second.selected {
			if first.reachability[firstTask][secondTask] {
				return fmt.Errorf("task %s (%s) is sequenced before task %s (%s)", firstTask.ID(), firstTask.Kind(), secondTask.ID(), secondTask.Kind())
			}
		}
	}
	return nil
}

// AssertLaneSuperset checks that the lanes of every task in superset contain
// all the lanes of every task in subset.
//
// This verifies that a failure of any task in superset includes every task in
// subset in its transactional rollback scope. The reverse is not necessarily
// true, as tasks in superset may belong to additional lanes.
func AssertLaneSuperset(withSupersetOfLanes, withSubsetOfLanes Selection) error {
	if err := validateNonEmpty(withSupersetOfLanes, withSubsetOfLanes); err != nil {
		return err
	}

	for _, supersetTask := range withSupersetOfLanes.selected {
		supersetLanes := laneSet(supersetTask)
		for _, subsetTask := range withSubsetOfLanes.selected {
			for _, lane := range subsetTask.Lanes() {
				if !supersetLanes[lane] {
					return fmt.Errorf("task %s (%s) with lanes %v is not a lane superset of task %s (%s) with lanes %v", supersetTask.ID(), supersetTask.Kind(), supersetTask.Lanes(), subsetTask.ID(), subsetTask.Kind(), subsetTask.Lanes())
				}
			}
		}
	}
	return nil
}

// AssertDoesNotShareLane checks that no task in first shares a lane with any
// task in second.
//
// This verifies that the two Selections are in independent transactional
// failure domains. A failure in one does not cause tasks in the other to be
// aborted or undone through shared lane membership, though task dependencies
// may still propagate the failure.
func AssertDoesNotShareLane(first, second Selection) error {
	if err := validateNonEmpty(first, second); err != nil {
		return err
	}

	for _, firstTask := range first.selected {
		firstLanes := laneSet(firstTask)
		for _, secondTask := range second.selected {
			for _, lane := range secondTask.Lanes() {
				if firstLanes[lane] {
					return fmt.Errorf("task %s (%s) and task %s (%s) share lane %d", firstTask.ID(), firstTask.Kind(), secondTask.ID(), secondTask.Kind(), lane)
				}
			}
		}
	}
	return nil
}

// AssertSameLanes checks that all tasks in the given Selections have exactly
// the same set of lanes.
//
// This verifies that the tasks have the same transactional failure domain and
// are included in the same rollback scope when any of them fails.
func AssertSameLanes(selections ...Selection) error {
	if err := validateNonEmpty(selections...); err != nil {
		return err
	}

	var reference *state.Task
	var referenceLanes map[int]bool
	for _, selection := range selections {
		for _, task := range selection.selected {
			if reference == nil {
				reference = task
				referenceLanes = laneSet(task)
				continue
			}

			taskLanes := laneSet(task)
			same := len(taskLanes) == len(referenceLanes)
			if same {
				for lane := range referenceLanes {
					if !taskLanes[lane] {
						same = false
						break
					}
				}
			}
			if !same {
				return fmt.Errorf("task %s (%s) has lanes %v, expected the same lanes as task %s (%s) with lanes %v", task.ID(), task.Kind(), task.Lanes(), reference.ID(), reference.Kind(), reference.Lanes())
			}
		}
	}
	return nil
}

func laneSet(task *state.Task) map[int]bool {
	lanes := make(map[int]bool, len(task.Lanes()))
	for _, lane := range task.Lanes() {
		lanes[lane] = true
	}
	return lanes
}

func validateNonEmpty(selections ...Selection) error {
	for i, selection := range selections {
		if len(selection.selected) == 0 {
			return fmt.Errorf("selection %d is empty", i+1)
		}
	}
	return nil
}
