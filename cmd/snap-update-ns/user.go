// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func applyUserFstab(snapName string) error {
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.user-fstab", dirs.SnapMountPolicyDir, snapName)
	desired, err := osutil.LoadMountProfile(desiredProfilePath)
	if err != nil {
		return fmt.Errorf("cannot load desired user mount profile of snap %q: %s", snapName, err)
	}

	// Replace XDG_RUNTIME_DIR in mount profile
	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	for i := range desired.Entries {
		if strings.HasPrefix(desired.Entries[i].Name, "$XDG_RUNTIME_DIR/") {
			desired.Entries[i].Name = strings.Replace(desired.Entries[i].Name, "$XDG_RUNTIME_DIR", xdgRuntimeDir, 1)
		}
		if strings.HasPrefix(desired.Entries[i].Dir, "$XDG_RUNTIME_DIR/") {
			desired.Entries[i].Dir = strings.Replace(desired.Entries[i].Dir, "$XDG_RUNTIME_DIR", xdgRuntimeDir, 1)
		}
	}

	debugShowProfile(desired, "desired mount profile")

	// TODO: configure the secure helper and inform it about directories that
	// can be created without trespassing.
	as := &Assumptions{}
	// TODO: Handle /home/*/snap/* when we do per-user mount namespaces and
	// allow defining layout items that refer to SNAP_USER_DATA and
	// SNAP_USER_COMMON.
	_, err = applyProfile(snapName, &osutil.MountProfile{}, desired, as)
	return err
}
