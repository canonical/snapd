// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package patch

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func init() {
	patches[4] = patch4
}

type patch4T struct{} // for namespacing of the helpers

func (patch4T) snapSetupAndState(task *state.Task) (*snapstate.SnapSetup, *snapstate.SnapState, error) {
	var snapst snapstate.SnapState

	ss, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot get snap setup from task %s (%s): %v", task.ID(), task.Kind(), err)
	}

	err = snapstate.Get(task.State(), ss.Name(), &snapst)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot get state for snap %q: %v", ss.Name(), err)
	}

	return ss, &snapst, err
}

func (patch4T) get(task *state.Task, key string, passThroughMissing bool, value interface{}) error {
	err := task.Get(key, value)
	if err == nil || (passThroughMissing && err == state.ErrNoState) {
		return err
	}
	change := task.Change()

	return fmt.Errorf("cannot get %q from task %s (%s) of change %s (%s): %v",
		key, task.ID(), task.Kind(), change.ID(), change.Kind(), err)
}

func (p4 patch4T) addCleanup(task *state.Task) error {
	// NOTE we could check for the status of the change itself, but
	// copy-snap-data is the one creating the trash, so if it's run there's
	// no sense in fiddling with the change.
	if task.Status().Ready() {
		return nil
	}

	ss, snapst, err := p4.snapSetupAndState(task)
	if err != nil {
		return err
	}
	if !snapst.HasCurrent() {
		// cleanup not added if no current (or if reverting, but
		// copy-snap-data was not done if reverting)
		return nil
	}

	var tid string
	if err := p4.get(task, "snap-setup-task", false, &tid); err != nil {
		return err
	}

	change := task.Change()
	revisionStr := ""
	if ss.SideInfo != nil {
		revisionStr = fmt.Sprintf(" (%s)", ss.Revision())
	}

	tasks := change.Tasks()
	last := tasks[len(tasks)-1]
	newTask := task.State().NewTask("cleanup", fmt.Sprintf("Clean up %q%s install", ss.Name(), revisionStr))
	newTask.Set("snap-setup-task", tid)
	newTask.WaitFor(last)
	change.AddTask(newTask)

	return nil
}

func (p4 patch4T) mangle(task *state.Task) error {
	ss, snapst, err := p4.snapSetupAndState(task)
	if err != nil {
		return err
	}

	var hadCandidate bool
	if err := p4.get(task, "had-candidate", false, &hadCandidate); err != nil {
		return err
	}

	if hadCandidate {
		change := task.Change()
		if change.Kind() != "revert-snap" {
			return fmt.Errorf("had-candidate true for task %s (%s) of non-revert change %s (%s)",
				task.ID(), task.Kind(), change.ID(), change.Kind())
		}
	}

	task.Clear("had-candidate")

	task.Set("old-candidate-index", snapst.LastIndex(ss.SideInfo.Revision))

	return nil
}

func (p4 patch4T) addRevertFlag(task *state.Task) error {
	var ss snapstate.SnapSetup
	err := p4.get(task, "snap-setup", true, &ss)
	switch err {
	case nil:
		ss.Flags |= snapstate.SnapSetupFlagRevert

		// save it back
		task.Set("snap-setup", &ss)
		return nil
	case state.ErrNoState:
		return nil
	default:
		return err
	}
}

func patch4(s *state.State) error {
	p4 := patch4T{}
	for _, change := range s.Changes() {
		if change.Kind() != "revert-snap" {
			continue
		}
		for _, task := range change.Tasks() {
			if err := p4.addRevertFlag(task); err != nil {
				return err
			}
		}
	}

	for _, task := range s.Tasks() {
		switch task.Kind() {
		case "link-snap":
			if err := p4.mangle(task); err != nil {
				return err
			}
		case "copy-snap-data":
			if err := p4.addCleanup(task); err != nil {
				return err
			}
		}
	}

	return nil
}
