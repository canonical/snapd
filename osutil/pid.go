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

package osutil

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SnapFromPid finds snap name running under given pid
func SnapFromPid(pid int, globalRootDir string) (string, error) {
	f, err := os.Open(fmt.Sprintf("%s/proc/%d/cgroup", globalRootDir, pid))
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// we need to find a string like:
		//   ...
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
		if !strings.Contains(controllerList, "freezer") {
			continue
		}
		if strings.HasPrefix(cgroupPath, "/snap.") {
			snap := strings.SplitN(filepath.Base(cgroupPath), ".", 2)[1]
			return snap, nil
		}
	}
	if scanner.Err() != nil {
		return "", scanner.Err()
	}

	return "", fmt.Errorf("cannot find a snap for pid %v", pid)
}
