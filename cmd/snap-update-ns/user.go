// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2019 Canonical Ltd
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
	"path/filepath"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// UserProfileUpdateContext contains information about update to per-user mount namespace.
type UserProfileUpdateContext struct {
	CommonProfileUpdateContext
	// uid is the numeric user identifier associated with the user for which
	// the update operation is occurring. It may be the current UID but doesn't
	// need to be.
	uid int
	// home contains the user's real home directory
	home      string
	homeError error
}

// isPlausibleHome returns an error if the path is empty, not clean, not absolute or cannot
// be opened for reading.
//
// See bootstrap.c function switch_to_privileged_user(). When snap-update-ns is invoked for
// user mounts, the effective uid and gid is changed to the calling user and supplementary
// groups dropped, while retaining capability SYS_ADMIN. Having the effective uid and gid
// changed to the calling user is a prerequisite for isPlausibleHome to function as intended.
func isPlausibleHome(path string) error {
	if path == "" {
		return fmt.Errorf("cannot allow empty path")
	}
	if path != filepath.Clean(path) {
		return fmt.Errorf("cannot allow unclean path")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("cannot allow relative path")
	}
	const openFlags = syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_DIRECTORY
	fd := mylog.Check2(sysOpen(path, openFlags, 0))

	sysClose(fd)
	return nil
}

// NewUserProfileUpdateContext returns encapsulated information for performing a per-user mount namespace update.
func NewUserProfileUpdateContext(instanceName string, fromSnapConfine bool, uid int) (*UserProfileUpdateContext, error) {
	realHome := os.Getenv("SNAP_REAL_HOME")
	var realHomeError error
	if realHome == "" {
		realHomeError = fmt.Errorf("cannot retrieve home directory")
	}
	mylog.Check(isPlausibleHome(realHome))

	return &UserProfileUpdateContext{
		CommonProfileUpdateContext: CommonProfileUpdateContext{
			instanceName:       instanceName,
			fromSnapConfine:    fromSnapConfine,
			currentProfilePath: currentUserProfilePath(instanceName, uid),
			desiredProfilePath: desiredUserProfilePath(instanceName),
		},
		uid:       uid,
		home:      realHome,
		homeError: realHomeError,
	}, nil
}

// Lock acquires locks / freezes needed to synchronize mount namespace changes.
func (upCtx *UserProfileUpdateContext) Lock() (unlock func(), err error) {
	// TODO: when persistent user mount namespaces are enabled, grab a lock
	// protecting the snap and freeze snap processes here.
	return func() {}, nil
}

// Assumptions returns information about file system mutability rules.
func (upCtx *UserProfileUpdateContext) Assumptions() *Assumptions {
	// TODO: configure the secure helper and inform it about directories that
	// can be created without trespassing.
	as := &Assumptions{}
	if upCtx.homeError == nil {
		as.AddUnrestrictedPaths(upCtx.home)
	}
	// TODO: Handle /home/*/snap/* when we do per-user mount namespaces and
	// allow defining layout items that refer to SNAP_USER_DATA and
	// SNAP_USER_COMMON.
	return as
}

// LoadDesiredProfile loads the desired, per-user mount profile, expanding user-specific variables.
func (upCtx *UserProfileUpdateContext) LoadDesiredProfile() (*osutil.MountProfile, error) {
	profile := mylog.Check2(upCtx.CommonProfileUpdateContext.LoadDesiredProfile())

	home := func() (path string, err error) {
		return upCtx.home, upCtx.homeError
	}
	mylog.Check(expandHomeDir(profile, home))

	// TODO: when SNAP_USER_DATA, SNAP_USER_COMMON or other variables relating
	// to the user name and their home directory need to be expanded then
	// handle them here.
	expandXdgRuntimeDir(profile, upCtx.uid)
	return profile, nil
}

// SaveCurrentProfile does nothing at all.
//
// Per-user mount profiles are not persisted yet.
func (upCtx *UserProfileUpdateContext) SaveCurrentProfile(profile *osutil.MountProfile) error {
	// TODO: when persistent user mount namespaces are enabled save the
	// current, per-user mount profile here.
	return nil
}

// LoadCurrentProfile returns the empty profile.
//
// Per-user mount profiles are not persisted yet.
func (upCtx *UserProfileUpdateContext) LoadCurrentProfile() (*osutil.MountProfile, error) {
	// TODO: when persistent user mount namespaces are enabled load the
	// current, per-user mount profile here.
	return &osutil.MountProfile{}, nil
}

// desiredUserProfilePath returns the path of the fstab-like file with the desired, user-specific mount profile for a snap.
func desiredUserProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.user-fstab", dirs.SnapMountPolicyDir, snapName)
}

// currentUserProfilePath returns the path of the fstab-like file with the applied, user-specific mount profile for a snap.
func currentUserProfilePath(snapName string, uid int) string {
	return fmt.Sprintf("%s/snap.%s.%d.user-fstab", dirs.SnapRunNsDir, snapName, uid)
}
