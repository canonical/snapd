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
	"github.com/snapcore/snapd/snap"
)

var (
	osutilSaveMountProfile = osutil.SaveMountProfile
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
	as := &Assumptions{}
	instanceName := upCtx.InstanceName()
	as.AddUnrestrictedPaths("/tmp", "/var/snap", "/snap/"+instanceName, "/dev/shm", "/run/systemd")
	if snapName := snap.InstanceSnap(instanceName); snapName != instanceName {
		as.AddUnrestrictedPaths("/snap/" + snapName)
	}
	// Allow the driver-libs interfaces (interfaces/builtin/*_driver_libs.go,
	// see the assemblyRoot constant in interfaces/builtin/helpers.go) to create
	// their assembly-tree skeleton directly under /run/snapd/snap (mounted as a
	// private tmpfs, see mountAssemblyRoot; assemblyRoot itself,
	// /run/snapd/snap/interfaces, is just its current sole subdirectory)
	// without tripping the trespassing check on the ancestor /run itself. /run
	// is a live host directory (sockets, other daemons' runtime state) that
	// the writable-mimic mechanism cannot safely reconstruct (it only supports
	// dirs, regular files and symlinks - see planWritableMimic), so unlike
	// /snap/$SNAP_NAME above, this directory's content is NOT mimic'd into a
	// private per-snap copy: creating it directly here is intentional and
	// mirrors the /run/systemd entry below. Only mount *targets* are ever
	// placed here by driver-libs; connecting snaps still see the bind-mounted
	// library content only within their own mount namespace.
	as.AddUnrestrictedPaths(dirs.SnapRuntimeAssemblyRoot)
	// Allow snap-update-ns to write to host's /tmp directory. This is
	// specifically here to allow two snaps to share X11 sockets that are placed
	// in the /tmp/.X11-unix/ directory in the private /tmp directories provided
	// by snap-confine. The X11 interface cannot offer a precise permission for
	// the slot-side snap, as there is no mechanism to convey this information.
	// As such, provide write access to all of /tmp.
	as.AddUnrestrictedPaths("/var/lib/snapd/hostfs/tmp")
	as.AddModeHint("/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.*", 0700)
	as.AddModeHint("/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.*/tmp", 0777|os.ModeSticky)
	// This is to ensure that unprivileged users can create the socket. This
	// permission only matters if the plug-side app constructs its mount
	// namespace before the slot-side app is launched.
	as.AddModeHint("/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.*/tmp/.X11-unix", 0777|os.ModeSticky)
	// This is to ensure the mount target directory /tmp/.X11-unix inside the
	// mount namespace is created with the same permissions as a typical host
	// /tmp/.X11-unix. When x11 interface is connected, the target path is
	// shadowed by a bind mount from the host, so the actual mode is only
	// visible when said interface is disconnected after being connected earlier
	// with the mount namespace being preserved.
	as.AddModeHint("/tmp/.X11-unix", 0777|os.ModeSticky)
	// This is to ensure private shared-memory directories have
	// the right permissions.
	as.AddModeHint("/dev/shm/snap.*", 0777|os.ModeSticky)
	return as
}

// SaveCurrentProfile saves the current mount profile to a file owned by the
// root user.
func (upCtx *SystemProfileUpdateContext) SaveCurrentProfile(profile *osutil.MountProfile) error {
	if err := osutilSaveMountProfile(profile, upCtx.currentProfilePath, 0, 0); err != nil {
		return fmt.Errorf("cannot save current mount profile of snap %q: %s", upCtx.instanceName, err)
	}
	return nil
}

// desiredSystemProfilePath returns the path of the fstab-like file with the desired, system-wide mount profile for a snap.
func desiredSystemProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)
}

// currentSystemProfilePath returns the path of the fstab-like file with the applied, system-wide mount profile for a snap.
func currentSystemProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
}
