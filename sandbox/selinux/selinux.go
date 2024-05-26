// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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

package selinux

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
)

// LevelType encodes the state of SELinux support found on this system.
type LevelType int

const (
	// SELinux is not supported
	Unsupported LevelType = iota
	// SELinux is supported and in permissive mode
	Permissive
	// SELinux is supported and in enforcing mode
	Enforcing
)

var (
	selinuxIsEnabled   = IsEnabled
	selinuxIsEnforcing = IsEnforcing
)

// ProbedLevel tells what level of SELinux enforcement is currently used
func ProbedLevel() LevelType {
	level, _ := probeSELinux()
	return level
}

// Summary describes SELinux status
func Summary() string {
	_, summary := probeSELinux()
	return summary
}

// Status returns the current level of SELinux support and a descriptive summary
func Status() (level LevelType, summary string) {
	return probeSELinux()
}

func probeSELinux() (LevelType, string) {
	enabled := mylog.Check2(selinuxIsEnabled())

	if !enabled {
		return Unsupported, ""
	}

	enforcing := mylog.Check2(selinuxIsEnforcing())

	if !enforcing {
		return Permissive, "SELinux is enabled but in permissive mode"
	}
	return Enforcing, "SELinux is enabled and in enforcing mode"
}

// MockIsEnabled makes the system believe a certain SELinux state is currently
// true
func MockIsEnabled(isEnabled func() (bool, error)) (restore func()) {
	old := selinuxIsEnabled
	selinuxIsEnabled = isEnabled
	return func() {
		selinuxIsEnabled = old
	}
}

// MockIsEnforcing makes the system believe the current SELinux is currently
// enforcing
func MockIsEnforcing(isEnforcing func() (bool, error)) (restore func()) {
	old := selinuxIsEnforcing
	selinuxIsEnforcing = isEnforcing
	return func() {
		selinuxIsEnforcing = old
	}
}
