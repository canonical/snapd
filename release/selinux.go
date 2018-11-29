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
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
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

// probe related code
var (
	selinuxSysPath = "sys/fs/selinux"
)

func probeSELinux() (SELinuxLevelType, string) {
	if !isDirectory(selinuxSysPath) {
		return NoSELinux, "SELinux not enabled"
	}

	rawState, err := ioutil.ReadFile(filepath.Join(selinuxSysPath, "enforce"))
	if err != nil {
		return NoSELinux, fmt.Sprintf("SELinux status cannot be determined: %v", err)
	}
	switch {
	case bytes.Equal(rawState, []byte("0")):
		return SELinuxPermissive, "SELinux is enabled and in permissive mode"
	case bytes.Equal(rawState, []byte("1")):
		return SELinuxEnforcing, "SELinux is enabled and in enforcing mode"
	}
	return NoSELinux, fmt.Sprintf("SELinux present but status cannot be determined: %s", rawState)
}
