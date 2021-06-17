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
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

const defaultFreezerCgroupV1Dir = "/sys/fs/cgroup/freezer"

var freezerCgroupV1Dir = defaultFreezerCgroupV1Dir

func init() {
	dirs.AddRootDirCallback(func(root string) {
		freezerCgroupV1Dir = filepath.Join(root, defaultFreezerCgroupV1Dir)
	})
}

func freezerV1Impl() {
	FreezeSnapProcesses = freezeSnapProcessesImplV1
	ThawSnapProcesses = thawSnapProcessesImplV1
}

func freezerV2Impl() {
	FreezeSnapProcesses = freezeSnapProcessesImplV2
	ThawSnapProcesses = thawSnapProcessesImplV2
}

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
var FreezeSnapProcesses = freezeSnapProcessesImplV1

// ThawSnapProcesses resumes execution of all processes belonging to a given snap.
//
// This operation can be mocked with MockFreezing
var ThawSnapProcesses = thawSnapProcessesImplV1

// freezeSnapProcessesImplV1 freezes all the processes originating from the given snap.
// Processes are frozen regardless of which particular snap application they
// originate from.
func freezeSnapProcessesImplV1(snapName string) error {
	fname := filepath.Join(freezerCgroupV1Dir, fmt.Sprintf("snap.%s", snapName), "freezer.state")
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

func thawSnapProcessesImplV1(snapName string) error {
	fname := filepath.Join(freezerCgroupV1Dir, fmt.Sprintf("snap.%s", snapName), "freezer.state")
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

func applyToSnap(snapName string, action func(groupName string) error) error {
	canary := fmt.Sprintf("snap.%s.", snapName)
	cgroupRoot := filepath.Join(rootPath, cgroupMountPoint)
	if _, dir, _ := osutil.DirExists(cgroupRoot); !dir {
		return nil
	}
	return filepath.Walk(filepath.Join(rootPath, cgroupMountPoint), func(name string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			return nil
		}
		if !strings.HasPrefix(info.Name(), canary) {
			return nil
		}
		// found a group
		if err := action(name); err != nil {
			return err
		}
		return filepath.SkipDir
	})
}

// freezeSnapProcessesImplV1 freezes all the processes originating from the given snap.
// Processes are frozen regardless of which particular snap application they
// originate from.
func freezeSnapProcessesImplV2(snapName string) error {
	freezeOne := func(dir string) error {
		fname := filepath.Join(dir, "cgroup.freeze")
		if err := ioutil.WriteFile(fname, []byte("1"), 0644); err != nil && os.IsNotExist(err) {
			//  the group may be gone already
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
			if !bytes.Equal(bytes.TrimSpace(data), []byte("1")) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		return nil
	}
	err := applyToSnap(snapName, freezeOne)
	if err == nil {
		return nil
	}
	// thaw one by one?
	ThawSnapProcesses(snapName) // ignore the error, this is best-effort.
	return fmt.Errorf("cannot finish freezing processes of snap %q: %v", snapName, err)
}

func thawSnapProcessesImplV2(snapName string) error {
	thawOne := func(dir string) error {
		fname := filepath.Join(dir, "cgroup.freeze")
		if err := ioutil.WriteFile(fname, []byte("0"), 0644); err != nil && os.IsNotExist(err) {
			//  the group may be gone already
			return nil
		} else if err != nil {
			return fmt.Errorf("cannot thaw processes of snap %q, %v", snapName, err)
		}
		return nil
	}
	return applyToSnap(snapName, thawOne)
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
