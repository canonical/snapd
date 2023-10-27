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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

const defaultFreezerCgroupV1Dir = "/sys/fs/cgroup/freezer"

var freezerCgroupV1Dir = defaultFreezerCgroupV1Dir

func init() {
	dirs.AddRootDirCallback(func(root string) {
		freezerCgroupV1Dir = filepath.Join(root, defaultFreezerCgroupV1Dir)
	})
}

func pickFreezerV1Impl() {
	FreezeSnapProcesses = freezeSnapProcessesImplV1
	ThawSnapProcesses = thawSnapProcessesImplV1
}

func pickFreezerV2Impl() {
	FreezeSnapProcesses = freezeSnapProcessesImplV2
	ThawSnapProcesses = thawSnapProcessesImplV2
}

// FreezeSnapProcesses suspends execution of all the processes belonging to
// a given snap. Processes remain frozen until ThawSnapProcesses is called,
// care must be taken not to freezer processes indefinitely.
//
// The freeze operation is not instant. Once commenced it proceeds
// asynchronously. Internally the function waits for the freezing to complete
// in at most 3000ms. If this time is insufficient then the processes are
// thawed and an error is returned.
//
// A correct implementation is picked depending on cgroup v1 or v2 use in the
// system. When cgroup v1 is detected, the call will directly act on the freezer
// group created when a snap process was started, while with v2 the call will
// act on all tracking groups of a snap.
//
// This operation can be mocked with MockFreezing
var FreezeSnapProcesses = freezeSnapProcessesImplV1

// ThawSnapProcesses resumes execution of all processes belonging to a given snap.
//
// A correct implementation is picked depending on cgroup v1 or v2 use in the
// system. When cgroup v1 is detected, the call will directly act on the freezer
// group created when a snap process was started, while with v2 the call will
// act on all tracking groups of a snap.
//
// This operation can be mocked with MockFreezing
var ThawSnapProcesses = thawSnapProcessesImplV1

// freezeSnapProcessesImplV1 freezes all the processes originating from the given snap.
// Processes are frozen regardless of which particular snap application they
// originate from.
func freezeSnapProcessesImplV1(snapName string) error {
	fname := filepath.Join(freezerCgroupV1Dir, fmt.Sprintf("snap.%s", snapName), "freezer.state")
	if err := os.WriteFile(fname, []byte("FROZEN"), 0644); err != nil && os.IsNotExist(err) {
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
	if err := os.WriteFile(fname, []byte("THAWED"), 0644); err != nil && os.IsNotExist(err) {
		// When there's no freezer cgroup we don't have to thaw anything.
		// This can happen when no process belonging to a given snap has been
		// started yet.
		return nil
	} else if err != nil {
		return fmt.Errorf("cannot thaw processes of snap %q", snapName)
	}
	return nil
}

func applyToSnap(snapName string, action func(groupName string) error, skipError func(err error) bool) error {
	if action == nil {
		return fmt.Errorf("internal error: action is nil")
	}
	if skipError == nil {
		return fmt.Errorf("internal error: skip error is nil")
	}
	canary := fmt.Sprintf("snap.%s.", snapName)
	cgroupRoot := filepath.Join(rootPath, cgroupMountPoint)
	if _, dir, _ := osutil.DirExists(cgroupRoot); !dir {
		return nil
	}
	return filepath.Walk(filepath.Join(rootPath, cgroupMountPoint), func(name string, info os.FileInfo, err error) error {
		if err != nil {
			if skipError(err) {
				// we don't know whether it's a file or
				// directory, so just return nil instead
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return nil
		}
		if !strings.HasPrefix(info.Name(), canary) {
			return nil
		}
		// snap applications end up inside a cgroup related to a
		// service, or when ran standalone, a scope
		if ext := filepath.Ext(info.Name()); ext != ".scope" && ext != ".service" {
			return nil
		}
		// found a group
		if err := action(name); err != nil && !skipError(err) {
			return err
		}
		return filepath.SkipDir
	})
}

// writeExistingFile can be used as a drop-in replacement for os.WriteFile,
// but does not create a file when it does not exist
func writeExistingFile(where string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(where, os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, errW := f.Write(data)
	errC := f.Close()
	// pick the right error
	if errW != nil {
		return errW
	}
	return errC
}

// freezeSnapProcessesImplV2 freezes all the processes originating from the
// given snap. Processes are frozen regardless of which particular snap
// application they originate from.
func freezeSnapProcessesImplV2(snapName string) error {
	// in case of v2, the process calling this code, (eg. snap-update-ns)
	// may already be part of the trackign cgroup for particular snap, care
	// must be taken to not freeze ourselves
	ownGroup, err := cgroupProcessPathInTrackingCgroup(os.Getpid())
	if err != nil {
		return err
	}
	ownGroupDir := filepath.Join(rootPath, cgroupMountPoint, ownGroup)
	freezeOne := func(dir string) error {
		if dir == ownGroupDir {
			// let's not freeze ourselves
			logger.Debugf("freeze, skipping own group %v", dir)
			return nil
		}
		fname := filepath.Join(dir, "cgroup.freeze")
		if err := writeExistingFile(fname, []byte("1"), 0644); err != nil {
			if os.IsNotExist(err) {
				//  the group may be gone already
				return nil
			}
			return fmt.Errorf("cannot freeze processes of snap %q, %v", snapName, err)
		}
		for i := 0; i < 30; i++ {
			data, err := ioutil.ReadFile(fname)
			if err != nil {
				if os.IsNotExist(err) {
					// group may be gone
					return nil
				}
				return fmt.Errorf("cannot determine the freeze state of processes of snap %q, %v", snapName, err)
			}
			// If the cgroup is still freezing then wait a moment and try again.
			if bytes.Equal(bytes.TrimSpace(data), []byte("1")) {
				// we're done
				return nil
			}
			// add a bit of delay
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("cannot freeze processes of snap %q in group %v", snapName, filepath.Base(dir))
	}
	// freeze, skipping ENOENT errors
	err = applyToSnap(snapName, freezeOne, os.IsNotExist)
	if err == nil {
		return nil
	}
	// we either got here because we hit a timeout freezing snap processes
	// or some other error

	// ignore errors when thawing processes, this is best-effort.
	alwaysSkipError := func(_ error) bool { return true }
	thawSnapProcessesV2(snapName, alwaysSkipError)
	return fmt.Errorf("cannot finish freezing processes of snap %q: %v", snapName, err)
}

func thawSnapProcessesV2(snapName string, skipError func(error) bool) error {
	if skipError == nil {
		return fmt.Errorf("internal error: skip error is nil")
	}
	thawOne := func(dir string) error {
		fname := filepath.Join(dir, "cgroup.freeze")
		if err := writeExistingFile(fname, []byte("0"), 0644); err != nil && os.IsNotExist(err) {
			//  the group may be gone already
			return nil
		} else if err != nil && !skipError(err) {
			return fmt.Errorf("cannot thaw processes of snap %q, %v", snapName, err)
		}
		return nil
	}
	return applyToSnap(snapName, thawOne, skipError)
}

func thawSnapProcessesImplV2(snapName string) error {
	// thaw skipping ENOENT errors
	return thawSnapProcessesV2(snapName, os.IsNotExist)
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
