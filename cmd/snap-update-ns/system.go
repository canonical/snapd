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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
)

// SystemProfileUpdateContext contains information about update to system-wide mount namespace.
type SystemProfileUpdateContext struct {
	CommonProfileUpdateContext
}

// NewSystemProfileUpdateContext returns encapsulated information for performing a per-user mount namespace update.
func NewSystemProfileUpdateContext(instanceName string, fromSnapConfine bool) *SystemProfileUpdateContext {
	return &SystemProfileUpdateContext{CommonProfileUpdateContext: CommonProfileUpdateContext{
		instanceName:       instanceName,
		fromSnapConfine:    fromSnapConfine,
		currentProfilePath: currentSystemProfilePath(instanceName),
		desiredProfilePath: desiredSystemProfilePath(instanceName),
	}}
}

// Assumptions returns information about file system mutability rules.
//
// System mount profiles can write to /tmp (this is required for constructing
// writable mimics) to /var/snap (where $SNAP_DATA is for services), /snap/$SNAP_NAME,
// and, in case of instances, /snap/$SNAP_INSTANCE_NAME.
func (upCtx *SystemProfileUpdateContext) Assumptions() *Assumptions {
	// Allow creating directories related to this snap name.
	//
	// Note that we allow /var/snap instead of /var/snap/$SNAP_NAME because
	// content interface connections can readily create missing mount points on
	// both sides of the interface connection.
	//
	// We scope /snap/$SNAP_NAME because only one side of the connection can be
	// created, as snaps are read-only, the mimic construction will kick-in and
	// create the missing directory but this directory is only visible from the
	// snap that we are operating on (either plug or slot side, the point is,
	// the mount point is not universally visible).
	//
	// /snap/$SNAP_NAME needs to be there as the code that creates such mount
	// points must traverse writable host filesystem that contains /snap/*/ and
	// normally such access is off-limits. This approach allows /snap/foo
	// without allowing /snap/bin, for example.
	//
	// /snap/$SNAP_INSTANCE_NAME and /snap/$SNAP_NAME are added to allow
	// remapping for parallel installs only when the snap has an instance key
	//
	// /var/lib/snapd/hostfs/var is there to allow the content interface to
	// bridge to arbitrary sub-directories of /var from the host, even if they
	// don't exist (they are created on the host in such case).
	as := &Assumptions{}
	instanceName := upCtx.InstanceName()
	as.AddUnrestrictedPaths("/tmp", "/var/snap", "/snap/"+instanceName, "/var/lib/snapd/hostfs/var")
	if snapName := snap.InstanceSnap(instanceName); snapName != instanceName {
		as.AddUnrestrictedPaths("/snap/" + snapName)
	}
	return as
}

// desiredSystemProfilePath returns the path of the fstab-like file with the desired, system-wide mount profile for a snap.
func desiredSystemProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)
}

// currentSystemProfilePath returns the path of the fstab-like file with the applied, system-wide mount profile for a snap.
func currentSystemProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
}
