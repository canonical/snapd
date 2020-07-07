// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"fmt"
	"path/filepath"
	"strings"
)

func snapNameFromPidUsingTrackingCgroup(pid int) (string, error) {
	// Maybe we have application tracking and can use it?
	path, err := ProcessPathInTrackingCgroup(pid)
	if err != nil {
		return "", err
	}
	if parsedTag := securityTagFromCgroupPath(path); parsedTag != nil {
		return parsedTag.InstanceName(), nil
	}
	return "", fmt.Errorf("cannot find snap security tag")
}

func snapNameFromPidUsingFreezerCgroup(pid int) (string, error) {
	// This logic only makes sense with cgroup v1.
	if IsUnified() {
		return "", fmt.Errorf("not supported")
	}

	// Find the path in the freezer cgroup.
	group, err := ProcGroup(pid, MatchV1Controller("freezer"))
	if err != nil {
		return "", fmt.Errorf("cannot determine cgroup path of pid %v: %v", pid, err)
	}
	if !strings.HasPrefix(group, "/snap.") {
		return "", fmt.Errorf("cannot find a snap for pid %v", pid)
	}

	// Extract the snap name form the path.
	snapName := strings.SplitN(filepath.Base(group), ".", 2)[1]
	if snapName == "" {
		return "", fmt.Errorf("snap name in cgroup path is empty")
	}
	return snapName, nil
}

func SnapNameFromPid(pid int) (string, error) {
	if snapName, err := snapNameFromPidUsingTrackingCgroup(pid); err == nil {
		return snapName, nil
	}
	return snapNameFromPidUsingFreezerCgroup(pid)
}
