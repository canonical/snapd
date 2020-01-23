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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

type ProcessInfo struct {
	InstanceName string
	AppName      string
	HookName     string
}

var (
	cgroupSnapNameFromPid   = cgroup.SnapNameFromPid
	apparmorSnapNameFromPid = apparmor.SnapNameFromPid
)

func NameFromPid(pid int) (ProcessInfo, error) {
	snapName, err := cgroupSnapNameFromPid(pid)
	if err != nil {
		return ProcessInfo{}, err
	}

	info := ProcessInfo{InstanceName: snapName}
	// If the process is confined by AppArmor, we can determine
	// more information about it.  We trust the label if it looks
	// like one snapd created and the snap name matches what we
	// got from the freezer cgroup.
	if snapName, appName, _, err := apparmorSnapNameFromPid(pid); err == nil {
		if snapName == info.InstanceName {
			info.AppName = appName
		} else {
			logger.Noticef("AppArmor snap name %q does not match cgroup snap name %q", snapName, info.InstanceName)
		}
	}

	return info, nil
}
