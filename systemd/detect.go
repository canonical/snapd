// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"os/exec"
)

// IsContainer returns true if the system is running in a container.
//
// The implementation calls: systemd-detect-virt --quiet --container.  It can
// be mocked with testutil.MockCommand. The zero exit code indicates that
// system _is_ running a container. Ensuring that --container is passed is
// important.
func IsContainer() bool {
	err := exec.Command("systemd-detect-virt", "--quiet", "--container").Run()
	return err == nil
}

// IsVirtualMachine returns true if the system is running in a virtual machine.
//
// The implementation calls: systemd-detect-virt --quiet --vm.  It can be mocked
// with testutil.MockCommand. The zero exit code indicates that system _is_
// running a vm. Ensuring that --vm is passed is important.
func IsVirtualMachine() bool {
	err := exec.Command("systemd-detect-virt", "--quiet", "--vm").Run()
	return err == nil
}
