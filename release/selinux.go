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

package release

import (
	"fmt"

	"github.com/snapcore/snapd/selinux"
)

// SELinuxLevelType encodes the state of SELinux support found on this system.
type SELinuxLevelType int

const (
	// NoSELinux indicates that SELinux is not enabled
	NoSELinux SELinuxLevelType = iota
	// SELinux is supported and in permissive mode
	SELinuxPermissive
	// SELinux is supported and in enforcing mode
	SELinuxEnforcing
)

var (
	selinuxLevel   SELinuxLevelType
	selinuxSummary string

	selinuxIsEnabled   = selinux.IsEnabled
	selinuxIsEnforcing = selinux.IsEnforcing
)

func init() {
	selinuxLevel, selinuxSummary = probeSELinux()
}

// SELinuxLevel tells what level of SELinux enforcement is currently used
func SELinuxLevel() SELinuxLevelType {
	return selinuxLevel
}

// SELinuxSummary describes SELinux status
func SELinuxSummary() string {
	return selinuxSummary
}

func probeSELinux() (SELinuxLevelType, string) {
	enabled, err := selinuxIsEnabled()
	if err != nil {
		return NoSELinux, err.Error()
	}
	if !enabled {
		return NoSELinux, ""
	}

	enforcing, err := selinuxIsEnforcing()
	if err != nil {
		return NoSELinux, fmt.Sprintf("SELinux is enabled, but status cannot be determined: %v", err)
	}
	if !enforcing {
		return SELinuxPermissive, "SELinux is enabled and in permissive mode"
	}
	return SELinuxEnforcing, "SELinux is enabled and in enforcing mode"
}
