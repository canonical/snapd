// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
)

const (
	// from golang.org/x/sys/unix
	cgroup2SuperMagic = 0x63677270

	defaultMountdir = "/sys/fs/cgroup"
)

const (
	// separate block, because iota is fun
	Unknown = iota
	V1
	V2
)

var (
	probeVersion       = Unknown
	probeErr     error = nil
)

func init() {
	probeVersion, probeErr = probeCgroupVersion()
}

var fsTypeForPath = fsTypeForPathImpl

func fsTypeForPathImpl(path string) (int64, error) {
	var statfs syscall.Statfs_t
	if err := syscall.Statfs(path, &statfs); err != nil {
		return 0, fmt.Errorf("cannot statfs path: %v", err)
	}
	return statfs.Type, nil
}

// ProcPath returns the path to the cgroup file under /proc for the given
// process id.
func ProcPath(pid int) string {
	return filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%v/cgroup", pid))
}

// ControllerPathV1 returns the path to given controller assuming cgroup v1
// hierarchy
func ControllerPathV1(controller string) string {
	return filepath.Join(dirs.GlobalRootDir, defaultMountdir, controller)
}

func probeCgroupVersion() (version int, err error) {
	cgroupMount := filepath.Join(dirs.GlobalRootDir, defaultMountdir)
	typ, err := fsTypeForPath(cgroupMount)
	if err != nil {
		return Unknown, fmt.Errorf("cannot determine filesystem type: %v", err)
	}
	if typ == cgroup2SuperMagic {
		return V2, nil
	}
	return V1, nil
}

// IsUnified returns true when a unified cgroup hierarchy is in use
func IsUnified() bool {
	version, _ := Version()
	return version == V2
}

// Version returns the detected cgroup version
func Version() (int, error) {
	return probeVersion, probeErr
}

// ProcGroup finds the path of a given cgroup controller for provided process
// id.
func ProcGroup(pid int, controller string) (string, error) {
	f, err := os.Open(ProcPath(pid))
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// we need to find a string like:
		//   ...
		//   <id>:<controller[,controller]>:/<path>
		//   7:freezer:/snap.hello-world
		//   ...
		// See cgroup(7) for details about the /proc/[pid]/cgroup
		// format.
		l := strings.Split(scanner.Text(), ":")
		if len(l) < 3 {
			continue
		}
		controllerList := l[1]
		cgroupPath := l[2]
		if !strings.Contains(controllerList, controller) {
			continue
		}

		return cgroupPath, nil
	}
	if scanner.Err() != nil {
		return "", scanner.Err()
	}

	return "", fmt.Errorf("cannot find cgroup controller %q path for pid %v", controller, pid)
}

// MockVersion sets the reported version of cgroup support. For use in testing only
func MockVersion(mockVersion int, mockErr error) (restore func()) {
	oldVersion, oldErr := probeVersion, probeErr
	probeVersion, probeErr = mockVersion, mockErr
	return func() {
		probeVersion, probeErr = oldVersion, oldErr
	}
}
