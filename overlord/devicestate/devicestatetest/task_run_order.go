// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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

package devicestatetest

import (
	"fmt"
	"errors"
	"sort"
	"strings"

	"github.com/snapcore/snapd/overlord/state"
)

// TaskPrintDeps print each task with its wait tasks.
func TaskPrintDeps(tsAll []*state.TaskSet) {
	// Add all tasks to single task list
	for i, ts := range tsAll {
		fmt.Printf("Taskset: [%d] with %d tasks, first task is: %s\n", i+1, len(ts.Tasks()), ts.Tasks()[0].Summary())
		for _, t := range ts.Tasks() {
			fmt.Printf("Task: [%s] %s\n", t.ID(), t.Summary())
			for _, wt := range t.WaitTasks() {
				fmt.Printf(" - Wait: %s\n", wt.ID())
			}
		}
	}
}

type TaskDependencyCycleError struct {
        IDs []string
        msg string
}

func (e *TaskDependencyCycleError) Error() string { return e.msg }

func (e *TaskDependencyCycleError) Is(err error) bool {
        _, ok := err.(*TaskDependencyCycleError)
        return ok
}

func successorTaskIDs(t *state.Task) []string {
    var taskIds []string
    for _, task := range t.HaltTasks() {      // Looping through tasks of type Task
        taskIds = append(taskIds, task.ID())  // Now task is of type Task, which has ID()
    }
    return taskIds
}

func TaskRunOrder2(c* state.Change) error {
	tasks := c.Tasks()
	taskLen := len(tasks)

	if taskLen == 0 {
		errors.New("Change is expected to have at least one task")
	}

	// group tasks by id and calculate predecessors for each task ID and 
	// identify tasks that are ready to run because they have no predecessors
	taskByID := map[string]*state.Task{}
	predecessorCount := make(map[string]int, taskLen)
	doingQueue := make([]string, 0, taskLen)
       	for _, t := range tasks {
		id := t.ID()
                taskByID[id] = t
                if l := len(t.WaitTasks()); l > 0 {
                        // only add an entry if the task is not independent
                        predecessorCount[id] = l
                } else {
			doingQueue = append(doingQueue, id)
		}
        }
	
	doQueue := make([]string, 0, taskLen - len(doingQueue))
	for len(doingQueue) > 0 {
		t := taskByID[doingQueue[0]]
		doingQueue = doingQueue[1:]
			
		var name string
		t.Get("instance-name", &name)
		fmt.Printf("ID: %-4s | Snap: %-14s | Kind: %-24s | Summary: %s\n", t.ID(), name, t.Kind(), t.Summary())

		// identify ready tasks
		for _, successorTaskId := range successorTaskIDs(t) {
			predecessorCount[successorTaskId]--
			if predecessorCount[successorTaskId] == 0 {
				delete(predecessorCount, successorTaskId)
				doQueue = append(doQueue, successorTaskId)
			}
		}

		// after servicing all parallel tasks load ready tasks
		if len(doingQueue) == 0 {
			doingQueue = append(doingQueue, doQueue...)
			doQueue = doQueue[:0]
		}
	}

	// report on dependency issues
        if len(predecessorCount) != 0 {
                // tasks that are left cannot have their dependencies satisfied
                var unsatisfiedTasks []string
                for id := range predecessorCount {
                        unsatisfiedTasks = append(unsatisfiedTasks, id)
                }
                sort.Strings(unsatisfiedTasks)
                msg := strings.Builder{}
                msg.WriteString("dependency cycle involving tasks [")
                for i, id := range unsatisfiedTasks {
                        t := taskByID[id]
                        msg.WriteString(fmt.Sprintf("%v:%v", t.ID(), t.Kind()))
                        if i < len(unsatisfiedTasks)-1 {
                                msg.WriteRune(' ')
                        }
                }
                msg.WriteRune(']')
                return &TaskDependencyCycleError{
                        IDs: unsatisfiedTasks,
                        msg: msg.String(),
                }
        }
        return nil
}

// TaskRunOrder returns tasks in the order that it will run.
func TaskRunOrder(tsAll []*state.TaskSet) ([]*state.Task, error) {
	var tasks []*state.Task

	// Add all tasks to single task list
	for _, ts := range tsAll {
		for _, t := range ts.Tasks() {
			tasks = append(tasks, t)
		}
	}

	var completedTasks []*state.Task
	var cycleCount int = 0
	// Repeatedly iterate over the tasklist until any of the following happens:
	//  - all tasks completed
	//  - no tasks completed in the cycle (dependency error)
	//  - multiple tasks completed in the cycle (dependency error)
	//  - reached cycle count limit of 10000 (settling error)
	for {
		// Iterate over the remaining tasks and check each task for completion.
		// A task is completed when all wait tasks are completed. Completed tasks
		// are removed from the iteration list and added to the completion list.
		// More than one task completing in the same iteration is considered
		// undeterministic and results in an error.

		var tasksCompletedSameTime []*state.Task
		var tasksCompletedSameTimeIndexes []int

		// Check remaining tasks for wait task completion
		for taskIndex, task := range tasks {
			//fmt.Printf("Inspecting task %s\n", task.ID())

			// Default to completed for no wait tasks
			var completed bool = true

			// Check if all wait tasks completed
			for _, waitTask := range task.WaitTasks() {
				completed = false
				//fmt.Printf(" - Wait task %s: ", waitTask.ID())
				for _, completedTask := range completedTasks {
					if waitTask.ID() == completedTask.ID() {
						completed = true
						//fmt.Printf("Done\n")
						break
					}
				}
				// Wait task not completed
				if !completed {
					//fmt.Printf("Pending\n")
					break
				}
			}
			// Task completed, all wait tasks done
			if completed {
				tasksCompletedSameTime = append(tasksCompletedSameTime, task)
				tasksCompletedSameTimeIndexes = append(tasksCompletedSameTimeIndexes, taskIndex)
			}
		}

		cycleCount++

		// Check for dependency errors
		if len(tasksCompletedSameTime) == 0 {
			return nil, fmt.Errorf("Dependency problem, no task completed\n")
		}
		if len(tasksCompletedSameTime) > 1 {
			fmt.Printf("Tasks completing at the same time:\n")
			for _, task := range tasksCompletedSameTime {
				fmt.Printf("ID: %-4s | Kind: %-30s | Summary: %s\n", task.ID(), task.Kind(), task.Summary())
			}
			return nil, fmt.Errorf("Dependency problem, %d tasks racing for completion\n", len(tasksCompletedSameTime))
		}

		for taskIndex, task := range tasksCompletedSameTime {
			var instanceName string
			task.Get("instance-name", &instanceName)
			fmt.Printf("ID: %-4s | Snap: %-14s | Kind: %-24s | Summary: %s\n", task.ID(), instanceName, task.Kind(), task.Summary())

			completedTasks = append(completedTasks, task)
			indexToRemove := tasksCompletedSameTimeIndexes[taskIndex]
			tasks = append(tasks[:indexToRemove], tasks[indexToRemove+1:]...)
		}

		if len(tasks) == 0 {
			//fmt.Printf("All tasks run in %d cycles\n", cycleCount)
			return completedTasks, nil
		}

		if cycleCount > 1000 {
			return nil, fmt.Errorf("Reached cycle limit")
		}
	}
}
