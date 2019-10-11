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

package snap

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

var appArmorLabelForPid = appArmorLabelForPidImpl

func appArmorLabelForPidImpl(pid int) (string, error) {
	procFile := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("/proc/%v/attr/current", pid))
	contents, err := ioutil.ReadFile(procFile)
	if os.IsNotExist(err) {
		return "unconfined", nil
	} else if err != nil {
		return "", err
	}
	label := strings.TrimRight(string(contents), "\n")
	// Trim off the mode
	if strings.HasSuffix(label, ")") {
		if pos := strings.LastIndex(label, " ("); pos != -1 {
			label = label[:pos]
		}
	}
	return label, nil
}

type ProcessInfo struct {
	InstanceName string
	AppName      string
	HookName     string
}

func decodeAppArmorLabel(label string) (ProcessInfo, error) {
	parts := strings.Split(label, ".")
	if parts[0] != "snap" {
		return ProcessInfo{}, fmt.Errorf("security label %q does not belong to a snap", label)
	}
	if len(parts) == 3 {
		return ProcessInfo{
			InstanceName: parts[1],
			AppName:      parts[2],
		}, nil
	}
	if len(parts) == 4 && parts[2] == "hook" {
		return ProcessInfo{
			InstanceName: parts[1],
			HookName:     parts[3],
		}, nil
	}
	return ProcessInfo{}, fmt.Errorf("unknown snap related security label %q", label)
}

var cgroupProcGroup = cgroup.ProcGroup

func NameFromPid(pid int) (ProcessInfo, error) {
	if cgroup.IsUnified() {
		// not supported
		return ProcessInfo{}, fmt.Errorf("not supported")
	}

	group, err := cgroupProcGroup(pid, cgroup.MatchV1Controller("freezer"))
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("cannot determine cgroup path of pid %v: %v", pid, err)
	}

	if !strings.HasPrefix(group, "/snap.") {
		return ProcessInfo{}, fmt.Errorf("cannot find a snap for pid %v", pid)
	}

	snapName := strings.SplitN(filepath.Base(group), ".", 2)[1]

	// If the process is confined by AppArmor, we can determine
	// more information about it.  We trust the label if it looks
	// like one snapd created and the snap name matches what we
	// got from the freezer cgroup.
	if label, err := appArmorLabelForPid(pid); err == nil {
		if info, err := decodeAppArmorLabel(label); err == nil {
			if info.InstanceName == snapName {
				return info, nil
			} else {
				logger.Noticef("AppArmor label %q does not match snap name %q", label, snapName)
			}
		}
	}

	return ProcessInfo{InstanceName: snapName}, nil
}
