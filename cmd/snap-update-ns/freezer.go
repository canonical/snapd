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

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

var freezerCgroupDir = "/sys/fs/cgroup/freezer"

// freezeSnapProcesses freezes all the processes originating from the given snap.
// Processes are frozen regardless of which particular snap application they
// originate from.
func freezeSnapProcesses(snapName string) error {
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
	thawSnapProcesses(snapName) // ignore the error, this is best-effort.
	return fmt.Errorf("cannot finish freezing processes of snap %q", snapName)
}

func thawSnapProcesses(snapName string) error {
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
