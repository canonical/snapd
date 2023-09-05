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

	"github.com/snapcore/snapd/overlord/state"
)

// TaskPrintDeps print each task with its wait tasks.
/*func TaskPrintDeps(tsAll []*state.TaskSet) {
	// Add all tasks to single task list
	for _, ts := range tsAll {
		for _, t := range ts.Tasks() {
			fmt.Printf("Task: [%s] %s\n", t.ID(), t.Summary())
			for _, wt := range t.WaitTasks() {
				fmt.Printf(" - Wait: %s\n", wt.ID())
			}
		}
	}
}*/

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
			return nil, fmt.Errorf("Dependency problem, %d tasks racing for completion\n", len(tasksCompletedSameTime))
		}

		for taskIndex, task := range tasksCompletedSameTime {
			//fmt.Printf("Done: [%s] %s\n", task.ID(), task.Summary())

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
