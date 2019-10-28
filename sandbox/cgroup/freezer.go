// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package cgroup

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

var freezerCgroupDir = "/sys/fs/cgroup/freezer"

// FreezeSnapProcessesImpl suspends execution of all the processes belonging to
// a given snap. Processes remain frozen until ThawSnapProcesses is called,
// care must be taken not to freezer processes indefinitely.
//
// The freeze operation is not instant. Once commenced it proceeds
// asynchronously. Internally the function waits for the freezing to complete
// in at most 3000ms. If this time is insufficient then the processes are
// thawed and an error is returned.
//
// This operation can be mocked with MockFreezing
var FreezeSnapProcesses = freezeSnapProcessesImpl

// ThawSnapProcesses resumes execution of all processes belonging to a given snap.
//
// This operation can be mocked with MockFreezing
var ThawSnapProcesses = thawSnapProcessesImpl

// freezeSnapProcessesImpl freezes all the processes originating from the given snap.
// Processes are frozen regardless of which particular snap application they
// originate from.
func freezeSnapProcessesImpl(snapName string) error {
	fname := filepath.Join(freezerCgroupDir, fmt.Sprintf("snap.%s", snapName), "freezer.state")
	if err := ioutil.WriteFile(fname, []byte("FROZEN"), 0644); err != nil && os.IsNotExist(err) {
		// When there's no freezer cgroup we don't have to freeze anything.
		// This can happen when no process belonging to a given snap has been
		// started yet.
		return nil
	} else if err != nil {
		return fmt.Errorf("cannot freeze processes of snap %q, %v", snapName, err)
	}
	for i := 0; i < 30; i++ {
		data, err := ioutil.ReadFile(fname)
		if err != nil {
			return fmt.Errorf("cannot determine the freeze state of processes of snap %q, %v", snapName, err)
		}
		// If the cgroup is still freezing then wait a moment and try again.
		if bytes.Equal(data, []byte("FREEZING")) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return nil
	}
	// If we got here then we timed out after seeing FREEZING for too long.
	ThawSnapProcesses(snapName) // ignore the error, this is best-effort.
	return fmt.Errorf("cannot finish freezing processes of snap %q", snapName)
}

func thawSnapProcessesImpl(snapName string) error {
	fname := filepath.Join(freezerCgroupDir, fmt.Sprintf("snap.%s", snapName), "freezer.state")
	if err := ioutil.WriteFile(fname, []byte("THAWED"), 0644); err != nil && os.IsNotExist(err) {
		// When there's no freezer cgroup we don't have to thaw anything.
		// This can happen when no process belonging to a given snap has been
		// started yet.
		return nil
	} else if err != nil {
		return fmt.Errorf("cannot thaw processes of snap %q", snapName)
	}
	return nil
}

// MockFreezing replaces the real implementation of freeze and thaw.
func MockFreezing(freeze, thaw func(snapName string) error) (restore func()) {
	oldFreeze := FreezeSnapProcesses
	oldThaw := ThawSnapProcesses

	FreezeSnapProcesses = freeze
	ThawSnapProcesses = thaw

	return func() {
		FreezeSnapProcesses = oldFreeze
		ThawSnapProcesses = oldThaw
	}
}
