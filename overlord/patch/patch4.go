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
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func init() {
	patches[4] = patch4
}

type patch4Flags int

type patch4DownloadInfo struct {
	AnonDownloadURL string `json:"anon-download-url,omitempty"`
	DownloadURL     string `json:"download-url,omitempty"`

	Size     int64  `json:"size,omitempty"`
	Sha3_384 string `json:"sha3-384,omitempty"`
}

type patch4SideInfo struct {
	RealName          string        `yaml:"name,omitempty" json:"name,omitempty"`
	SnapID            string        `yaml:"snap-id" json:"snap-id"`
	Revision          snap.Revision `yaml:"revision" json:"revision"`
	Channel           string        `yaml:"channel,omitempty" json:"channel,omitempty"`
	DeveloperID       string        `yaml:"developer-id,omitempty" json:"developer-id,omitempty"`
	Developer         string        `yaml:"developer,omitempty" json:"developer,omitempty"`
	EditedSummary     string        `yaml:"summary,omitempty" json:"summary,omitempty"`
	EditedDescription string        `yaml:"description,omitempty" json:"description,omitempty"`
	Private           bool          `yaml:"private,omitempty" json:"private,omitempty"`
}

type patch4SnapSetup struct {
	Channel      string              `json:"channel,omitempty"`
	UserID       int                 `json:"user-id,omitempty"`
	Flags        patch4Flags         `json:"flags,omitempty"`
	SnapPath     string              `json:"snap-path,omitempty"`
	DownloadInfo *patch4DownloadInfo `json:"download-info,omitempty"`
	SideInfo     *patch4SideInfo     `json:"side-info,omitempty"`
}

func (ss *patch4SnapSetup) Name() string {
	if ss.SideInfo.RealName == "" {
		panic("SnapSetup.SideInfo.RealName not set")
	}
	return ss.SideInfo.RealName
}

func (ss *patch4SnapSetup) Revision() snap.Revision {
	return ss.SideInfo.Revision
}

type patch4SnapState struct {
	SnapType string            `json:"type"` // Use Type and SetType
	Sequence []*patch4SideInfo `json:"sequence"`
	Active   bool              `json:"active,omitempty"`
	Current  snap.Revision     `json:"current"`
	Channel  string            `json:"channel,omitempty"`
	Flags    patch4Flags       `json:"flags,omitempty"`
}

func (snapst *patch4SnapState) LastIndex(revision snap.Revision) int {
	for i := len(snapst.Sequence) - 1; i >= 0; i-- {
		if snapst.Sequence[i].Revision == revision {
			return i
		}
	}
	return -1
}

type patch4T struct{} // for namespacing of the helpers

func (p4 patch4T) taskSnapSetup(task *state.Task) (*patch4SnapSetup, error) {
	var ss patch4SnapSetup

	switch err := p4.getMaybe(task, "snap-setup", &ss); err {
	case state.ErrNoState:
		// continue below
	case nil:
		return &ss, nil
	default:
		return nil, err
	}

	var id string
	if err := p4.get(task, "snap-setup-task", &id); err != nil {
		return nil, err
	}

	if err := p4.get(task.State().Task(id), "snap-setup", &ss); err != nil {
		return nil, err
	}

	return &ss, nil
}

func (p4 patch4T) snapSetupAndState(task *state.Task) (*patch4SnapSetup, *patch4SnapState, error) {
	var snapst patch4SnapState

	ss, err := p4.taskSnapSetup(task)
	if err != nil {
		return nil, nil, err
	}

	var snaps map[string]*json.RawMessage
	err = task.State().Get("snaps", &snaps)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot get snaps state: %v", err)
	}
	raw, ok := snaps[ss.Name()]
	if !ok {
		return nil, nil, fmt.Errorf("cannot get snap state for %q: %v", ss.Name(), err)
	}
	err = json.Unmarshal([]byte(*raw), &snapst)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot get state for snap %q: %v", ss.Name(), err)
	}

	return ss, &snapst, err
}

// getMaybe calls task.Get and wraps any non-ErrNoState error in an informative message
func (p4 patch4T) getMaybe(task *state.Task, key string, value interface{}) error {
	return p4.gget(task, key, true, value)
}

// get calls task.Get and wraps any error in an informative message
func (p4 patch4T) get(task *state.Task, key string, value interface{}) error {
	return p4.gget(task, key, false, value)
}

// gget does the actual work of get and getMaybe
func (patch4T) gget(task *state.Task, key string, passThroughMissing bool, value interface{}) error {
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

	ss, err := p4.taskSnapSetup(task)
	if err != nil {
		return err
	}

	var tid string
	if err := p4.get(task, "snap-setup-task", &tid); err != nil {
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
	if err := p4.getMaybe(task, "had-candidate", &hadCandidate); err != nil && err != state.ErrNoState {
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
	var ss patch4SnapSetup
	err := p4.getMaybe(task, "snap-setup", &ss)
	switch err {
	case nil:
		ss.Flags |= patch4Flags(snapstate.SnapSetupFlagRevert)

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
		// change is full done, take it easy
		if change.Status().Ready() {
			continue
		}

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
		// change is full done, take it easy
		if task.Change().Status().Ready() {
			continue
		}

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
