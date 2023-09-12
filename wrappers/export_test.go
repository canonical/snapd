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

package wrappers

import (
	"os/user"
	"time"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

// some internal helper exposed for testing
var (
	// dbus
	GenerateDBusActivationFile = generateDBusActivationFile

	// desktop
	SanitizeDesktopFile    = sanitizeDesktopFile
	RewriteExecLine        = rewriteExecLine
	RewriteIconLine        = rewriteIconLine
	IsValidDesktopFileLine = isValidDesktopFileLine

	// daemons
	UsersToUids               = usersToUids
	NewUserServiceClientNames = newUserServiceClientNames

	SanitizeSessionFile    = sanitizeSessionFile
	IsValidSessionFileLine = isValidSessionFileLine

	// icons
	FindIconFiles = findIconFiles
)

func MockKillWait(wait time.Duration) (restore func()) {
	oldKillWait := killWait
	killWait = wait
	return func() {
		killWait = oldKillWait
	}
}

func MockEnsureDirState(f func(dir string, glob string, content map[string]osutil.FileState) (changed, removed []string, err error)) (restore func()) {
	oldEnsureDirState := ensureDirState
	ensureDirState = f
	return func() {
		ensureDirState = oldEnsureDirState
	}
}

func MockUserLookup(f func(username string) (*user.User, error)) (restore func()) {
	restore = testutil.Backup(&userLookup)
	userLookup = f
	return restore
}
