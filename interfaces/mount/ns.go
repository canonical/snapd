// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package mount

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdtool"
)

// mountNsPath returns path of the mount namespace file of a given snap
func mountNsPath(snapName string) string {
	// NOTE: This value has to be synchronized with snap-confine
	return filepath.Join(dirs.SnapRunNsDir, fmt.Sprintf("%s.mnt", snapName))
}

// Run an internal tool on a given snap namespace, if one exists.
func runNamespaceTool(toolName, snapName string) ([]byte, error) {
	mntFile := mountNsPath(snapName)
	if osutil.FileExists(mntFile) {
		toolPath, err := snapdtool.InternalToolPath(toolName)
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(toolPath, snapName)
		output, err := cmd.CombinedOutput()
		return output, err
	}
	return nil, nil
}

// Discard the mount namespace of a given snap.
func DiscardSnapNamespace(snapName string) error {
	output, err := runNamespaceTool("snap-discard-ns", snapName)
	if err != nil {
		return fmt.Errorf("cannot discard preserved namespace of snap %q: %s", snapName, osutil.OutputErr(output, err))
	}
	return nil
}

// Update the mount namespace of a given snap.
func UpdateSnapNamespace(snapName string) error {
	output, err := runNamespaceTool("snap-update-ns", snapName)
	if err != nil {
		return fmt.Errorf("cannot update preserved namespace of snap %q: %s", snapName, osutil.OutputErr(output, err))
	}
	return nil
}
