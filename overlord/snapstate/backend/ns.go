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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// mountNsPath returns path of the mount namespace file of a given snap
func mountNsPath(snapName string) string {
	// NOTE: This value has to be synchronized with snap-confine
	return filepath.Join(dirs.SnapRunNsDir, fmt.Sprintf("%s.mnt", snapName))
}

func (b Backend) DiscardSnapNamespace(snapName string) error {
	mntFile := mountNsPath(snapName)
	// If there's a .mnt file that was created by snap-confine we should ask
	// snap-confine to discard it appropriately.
	if osutil.FileExists(mntFile) {
		snapDiscardNs := filepath.Join(dirs.LibExecDir, "snap-discard-ns")
		cmd := exec.Command(snapDiscardNs, snapName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot discard preserved namespaces of snap %q: %s", snapName, osutil.OutputErr(output, err))
		}
	}
	return nil
}
