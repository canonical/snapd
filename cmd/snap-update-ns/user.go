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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// UserProfileUpdate contains information about update to per-user mount namespace.
type UserProfileUpdate struct {
	CommonProfileUpdate
}

func applyUserFstab(snapName string) error {
	up := &UserProfileUpdate{}
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.user-fstab", dirs.SnapMountPolicyDir, snapName)
	desired, err := osutil.LoadMountProfile(desiredProfilePath)
	if err != nil {
		return fmt.Errorf("cannot load desired user mount profile of snap %q: %s", snapName, err)
	}

	expandXdgRuntimeDir(desired, os.Getuid())
	debugShowProfile(desired, "desired mount profile")

	// TODO: configure the secure helper and inform it about directories that
	// can be created without trespassing.
	as := &Assumptions{}
	// TODO: Handle /home/*/snap/* when we do per-user mount namespaces and
	// allow defining layout items that refer to SNAP_USER_DATA and
	// SNAP_USER_COMMON.
	_, err = applyProfile(up, snapName, &osutil.MountProfile{}, desired, as)
	return err
}
