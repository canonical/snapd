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

package backend

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

func (b Backend) HealthCheckStatic(snapName string, rev snap.Revision) error {
	checker := filepath.Join(snap.MountDir(snapName, rev), "meta/checks/static-check")
	if osutil.FileExists(checker) {
		// FIXME: this is curently unconfined, we need
		//   `snap run --health-check=static snap-name`
		output, err := exec.Command(checker).CombinedOutput()
		if err != nil {
			return fmt.Errorf("static health check for %q failed: %s", snapName, osutil.OutputErr(output, err))
		}
	}

	return nil
}
