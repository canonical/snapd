// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"github.com/snapcore/snapd/sandbox/cgroup"
)

var (
	ValidateSocketName           = validateSocketName
	ValidateDescription          = validateDescription
	ValidateTitle                = validateTitle
	InfoFromSnapYamlWithSideInfo = infoFromSnapYamlWithSideInfo

	AppArmorLabelForPidImpl = appArmorLabelForPidImpl
	DecodeAppArmorLabel     = decodeAppArmorLabel
)

func (info *Info) ForceRenamePlug(oldName, newName string) {
	info.forceRenamePlug(oldName, newName)
}

func NewScopedTracker() *scopedTracker {
	return new(scopedTracker)
}

func MockAppArmorLabelForPid(f func(pid int) (string, error)) (restore func()) {
	old := appArmorLabelForPid
	appArmorLabelForPid = f
	return func() {
		appArmorLabelForPid = old
	}
}

func MockProcGroup(f func(pid int, match cgroup.GroupMatcher) (string, error)) (restore func()) {
	old := cgroupProcGroup
	cgroupProcGroup = f
	return func() {
		cgroupProcGroup = old
	}
}
