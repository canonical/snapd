package tasktest

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/state"
)

func validateNonEmpty(selections ...Selection) error {
	for i, selection := range selections {
		if len(selection.tasks) == 0 {
			return fmt.Errorf("selection %d is empty", i+1)
		}
	}
	return nil
}

func AssertSequenced(selections ...Selection) error {
	if err := validateNonEmpty(selections...); err != nil {
		return err
	}

	for i := 0; i+1 < len(selections); i++ {
		before := selections[i]
		after := selections[i+1]
		for _, beforeTask := range before.tasks {
			for _, afterTask := range after.tasks {
				if !before.reachability[beforeTask][afterTask] {
					return fmt.Errorf("task %s (%s) is not sequenced before task %s (%s)", beforeTask.ID(), beforeTask.Kind(), afterTask.ID(), afterTask.Kind())
				}
			}
		}
	}
	return nil
}

func AssertNotSequenced(first, second Selection) error {
	if err := validateNonEmpty(first, second); err != nil {
		return err
	}

	for _, firstTask := range first.tasks {
		for _, secondTask := range second.tasks {
			if first.reachability[firstTask][secondTask] {
				return fmt.Errorf("task %s (%s) is sequenced before task %s (%s)", firstTask.ID(), firstTask.Kind(), secondTask.ID(), secondTask.Kind())
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

func AssertCommonLane(selections ...Selection) error {
	if err := validateNonEmpty(selections...); err != nil {
		return err
	}

	var common map[int]bool
	for _, selection := range selections {
		for _, task := range selection.tasks {
			if common == nil {
				common = laneSet(task)
				continue
			}

			taskLanes := laneSet(task)
			for lane := range common {
				if !taskLanes[lane] {
					delete(common, lane)
				}
			}
			if len(common) == 0 {
				return fmt.Errorf("task %s (%s) with lanes %v shares no lane with all preceding tasks", task.ID(), task.Kind(), task.Lanes())
			}
		}
	}
	return nil
}

func AssertLaneSuperset(superset, subset Selection) error {
	if err := validateNonEmpty(superset, subset); err != nil {
		return err
	}

	for _, supersetTask := range superset.tasks {
		supersetLanes := laneSet(supersetTask)
		for _, subsetTask := range subset.tasks {
			for _, lane := range subsetTask.Lanes() {
				if !supersetLanes[lane] {
					return fmt.Errorf("task %s (%s) with lanes %v is not a lane superset of task %s (%s) with lanes %v", supersetTask.ID(), supersetTask.Kind(), supersetTask.Lanes(), subsetTask.ID(), subsetTask.Kind(), subsetTask.Lanes())
				}
			}
		}
	}
	return nil
}

func AssertDoesNotShareLane(first, second Selection) error {
	if err := validateNonEmpty(first, second); err != nil {
		return err
	}

	for _, firstTask := range first.tasks {
		firstLanes := laneSet(firstTask)
		for _, secondTask := range second.tasks {
			for _, lane := range secondTask.Lanes() {
				if firstLanes[lane] {
					return fmt.Errorf("task %s (%s) and task %s (%s) share lane %d", firstTask.ID(), firstTask.Kind(), secondTask.ID(), secondTask.Kind(), lane)
				}
			}
		}
	}
	return nil
}

func AssertSameLanes(selections ...Selection) error {
	if err := validateNonEmpty(selections...); err != nil {
		return err
	}

	var reference *state.Task
	var referenceLanes map[int]bool
	for _, selection := range selections {
		for _, task := range selection.tasks {
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
