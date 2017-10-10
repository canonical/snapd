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
	"path/filepath"
	"time"
)

const freezerCgroup = "/sys/fs/cgroup/freezer"

func freezeSnapProcesses(snapName string) error {
	fname := filepath.Join(freezerCgroup, fmt.Sprintf("snap.%s", snapName), "freezer.state")
	if err := ioutil.WriteFile(fname, []byte("FROZEN"), 0644); err != nil {
		return fmt.Errorf("cannot freeze processes of snap %q, %v", snapName, err)
	}
	for i := 0; i < 10; i++ {
		data, err := ioutil.ReadFile(fname)
		if err != nil {
			return fmt.Errorf("cannot determine the freeze state of processes of snap %q, %v", snapName, err)
		}
		// If the cgroup is still freezing then wait a moment and try again.
		if bytes.Equal(data, []byte("FREEZING")) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		break
	}
	return nil
}

func thawSnapProcesses(snapName string) error {
	fname := filepath.Join(freezerCgroup, fmt.Sprintf("snap.%s", snapName), "freezer.state")
	if err := ioutil.WriteFile(fname, []byte("THAWED"), 0644); err != nil {
		return fmt.Errorf("cannot thaw processes of snap %q", snapName)
	}
	return nil
}
