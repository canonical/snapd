// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapd_kernel_probe

import (
	"fmt"
	"syscall"
)

// For mocking in tests.
// ensureOwnNetns returns an error unless the network namespace at selfPath
// differs from the one at parentPath. The probe is started by snapd with
// CLONE_NEWNET so that it owns a private network namespace; this protects
// the host from having its loopback interface touched if the tool is invoked
// by hand, or the clone flag is ever dropped. The comparison is by device and
// inode of the nsfs files, which is the identity of a namespace.
func ensureOwnNetns(selfPath, parentPath string) error {
	var self, parent syscall.Stat_t
	if err := syscall.Stat(selfPath, &self); err != nil {
		return fmt.Errorf("cannot inspect own network namespace: %v", err)
	}
	if err := syscall.Stat(parentPath, &parent); err != nil {
		return fmt.Errorf("cannot inspect parent network namespace: %v", err)
	}
	if self.Dev == parent.Dev && self.Ino == parent.Ino {
		return fmt.Errorf("refusing to configure loopback in the network namespace of the parent process")
	}
	return nil
}
