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
	. "gopkg.in/check.v1"
)

var (
	// change
	ValidateInstanceName = validateInstanceName
	ProcessArguments     = processArguments
	// freezer
	FreezeSnapProcesses = freezeSnapProcesses
	ThawSnapProcesses   = thawSnapProcesses
	// utils
	PlanWritableMimic = planWritableMimic
	ExecWritableMimic = execWritableMimic
	// main
	ComputeAndSaveChanges = computeAndSaveChanges
	ApplyUserFstab        = applyUserFstab
	// bootstrap
	ClearBootstrapError = clearBootstrapError
	// trespassing
	IsReadOnly                   = isReadOnly
	IsPrivateTmpfsCreatedBySnapd = isPrivateTmpfsCreatedBySnapd
)

func MockFreezerCgroupDir(c *C) (restore func()) {
	old := freezerCgroupDir
	freezerCgroupDir = c.MkDir()
	return func() {
		freezerCgroupDir = old
	}
}

func FreezerCgroupDir() string {
	return freezerCgroupDir
}

func MockChangePerform(f func(chg *Change, as *Assumptions) ([]*Change, error)) func() {
	origChangePerform := changePerform
	changePerform = f
	return func() {
		changePerform = origChangePerform
	}
}

func (as *Assumptions) IsRestricted(path string) bool {
	return as.isRestricted(path)
}

func (as *Assumptions) PastChanges() []*Change {
	return as.pastChanges
}
