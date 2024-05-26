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

package systemd

import (
	"fmt"
	"os/exec"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

var systemdSysctlPath = "/lib/systemd/systemd-sysctl"

var systemdSysctlCmd = func(args ...string) error {
	bs := mylog.Check2(exec.Command(systemdSysctlPath, args...).CombinedOutput())

	return nil
}

// Sysctl invokes systemd-sysctl to configure sysctl(8) kernel parameters
// from disk configuration. A set of prefixes can be passed to limit
// which settings are (re)configured.
func Sysctl(prefixes []string) error {
	args := []string{}
	for _, p := range prefixes {
		args = append(args, "--prefix", p)
	}
	return systemdSysctlCmd(args...)
}

// MockSystemdSysctl lets mock and intercept calls to systemd-sysctl
// from the package.
func MockSystemdSysctl(f func(args ...string) error) func() {
	oldsystemdSysctlCmd := systemdSysctlCmd
	systemdSysctlCmd = f
	return func() {
		systemdSysctlCmd = oldsystemdSysctlCmd
	}
}
