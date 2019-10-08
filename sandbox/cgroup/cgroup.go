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

	"github.com/snapcore/snapd/strutil"
)

const (
	// From golang.org/x/sys/unix
	cgroup2SuperMagic = 0x63677270

	// The only cgroup path we expect, for v2 this is where the unified
	// hierarchy is mounted, for v1 this is usually a tmpfs mount, under
	// which the controller-hierarchies are mounted
	expectedMountPoint = "/sys/fs/cgroup"
)

var (
	// Filesystem root defined locally to avoid dependency on the 'dirs'
	// package
	rootPath = "/"
)

const (
	// Separate block, because iota is fun
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
	// Typs is int32 on 386, use explicit conversion to keep the code
	// working for both
	return int64(statfs.Type), nil
}

// ProcPidPath returns the path to the cgroup file under /proc for the given
// process id.
func ProcPidPath(pid int) string {
	return filepath.Join(rootPath, fmt.Sprintf("proc/%v/cgroup", pid))
}

// ControllerPathV1 returns the path to given controller assuming cgroup v1
// hierarchy
func ControllerPathV1(controller string) string {
	return filepath.Join(rootPath, expectedMountPoint, controller)
}

func probeCgroupVersion() (version int, err error) {
	cgroupMount := filepath.Join(rootPath, expectedMountPoint)
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

// GroupSelector specifies the criteria for selecting the cgroup of a given
// process. All options are exclusive and cannot be combined, consult
// cgroups(7).
type GroupSelector struct {
	// Unified indicates a unified cgroup v2 hierarchy
	Unified bool
	// Controller name in a cgroup v1 hierarchy
	Controller string
	// Name of a named cgroup v1 hierarchy
	Name string
}

func (g GroupSelector) Valid() error {
	if g == (GroupSelector{}) {
		fmt.Errorf("an empty cgroup selector")
	}
	// error out on cases that are invalid as described in cgroups(7)
	if g.Unified {
		if g.Controller != "" {
			return fmt.Errorf("controller %q with a unified hierarchy", g.Controller)
		}
		if g.Name != "" {
			return fmt.Errorf("named hierarchy %q with a unified one", g.Name)
		}
	} else {
		if g.Name != "" && g.Controller != "" {
			return fmt.Errorf("named hierarchy %q with a controller %q", g.Name, g.Controller)
		}
	}
	return nil
}

func (g GroupSelector) String() string {
	if g.Controller != "" {
		return fmt.Sprintf("controller %q", g.Controller)
	}
	if g.Name != "" {
		return fmt.Sprintf("named hierarchy %q", g.Name)
	}
	if g.Unified {
		return "unified hierarchy"
	}
	return "invalid selector"
}

// Match checks whether provided id, controllers list tumple matches the
// selector
func (g GroupSelector) Match(id, maybeControllers string) bool {
	if g.Unified {
		// unified hierarchy format is always 0::<path>
		if id != "0" || maybeControllers != "" {
			return false
		}
	}
	if g.Controller != "" {
		controllerList := strings.Split(maybeControllers, ",")
		if !strutil.ListContains(controllerList, g.Controller) {
			return false
		}
	}
	if g.Name != "" {
		if !strings.HasPrefix(maybeControllers, "name=") {
			return false
		}
		name := strings.TrimPrefix(maybeControllers, "name=")
		if name != g.Name {
			return false
		}
	}
	return true
}

// ProcGroup finds the path of a given cgroup controller for provided process
// id.
func ProcGroup(pid int, selector GroupSelector) (string, error) {
	if err := selector.Valid(); err != nil {
		return "", fmt.Errorf("invalid group selector: %v", err)
	}

	f, err := os.Open(ProcPidPath(pid))
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
		// See cgroups(7) for details about the /proc/[pid]/cgroup
		// format.
		l := strings.Split(scanner.Text(), ":")
		if len(l) < 3 {
			continue
		}
		id := l[0]
		maybeControllerList := l[1]
		cgroupPath := l[2]

		if !selector.Match(id, maybeControllerList) {
			continue
		}

		return cgroupPath, nil
	}
	if scanner.Err() != nil {
		return "", scanner.Err()
	}

	return "", fmt.Errorf("cannot find %v cgroup path for pid %v", selector, pid)
}

// MockVersion sets the reported version of cgroup support. For use in testing only
func MockVersion(mockVersion int, mockErr error) (restore func()) {
	oldVersion, oldErr := probeVersion, probeErr
	probeVersion, probeErr = mockVersion, mockErr
	return func() {
		probeVersion, probeErr = oldVersion, oldErr
	}
}
