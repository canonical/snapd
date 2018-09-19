// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package selftest

import (
	"fmt"
	"os"
)

var apparmorProfilesPath = "/sys/kernel/security/apparmor/profiles"

func apparmorUsable() error {
	// Check that apparmor is actually usable. In some
	// configurations of lxd, apparmor looks available when in
	// reality it isn't. Eg, this can happen when a container runs
	// unprivileged (eg, root in the container is non-root
	// outside) and also unconfined (where lxd doesn't set up an
	// apparmor policy namespace). We can therefore simply check
	// if /sys/kernel/security/apparmor/profiles is readable (like
	// aa-status does), and if it isn't, we know we can't manipulate
	// policy.
	f, err := os.Open(apparmorProfilesPath)
	if os.IsPermission(err) {
		return fmt.Errorf("apparmor detected but insufficient permissions to use it")
	}
	if f != nil {
		f.Close()
	}
	return nil
}
