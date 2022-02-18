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
	"os/exec"
	"os/user"

	"github.com/snapcore/snapd/dirs"
)

var (
	AddMountUnit    = addMountUnit
	RemoveMountUnit = removeMountUnit
	RemoveIfEmpty   = removeIfEmpty
)

func MockUpdateFontconfigCaches(f func() error) (restore func()) {
	oldUpdateFontconfigCaches := updateFontconfigCaches
	updateFontconfigCaches = f
	return func() {
		updateFontconfigCaches = oldUpdateFontconfigCaches
	}
}

func MockCommandFromSystemSnap(f func(string, ...string) (*exec.Cmd, error)) (restore func()) {
	old := commandFromSystemSnap
	commandFromSystemSnap = f
	return func() {
		commandFromSystemSnap = old
	}
}

func MockAllUsers(f func(options *dirs.SnapDirOptions) ([]*user.User, error)) func() {
	old := allUsers
	allUsers = f
	return func() {
		allUsers = old
	}
}

func MockRemoveIfEmpty(f func(dir string) error) func() {
	old := removeIfEmpty
	removeIfEmpty = f
	return func() {
		removeIfEmpty = old
	}
}

func MockInitSnapMaybeFailForTesting(f func() error) func() {
	old := maybeFailForTesting
	maybeFailForTesting = f
	return func() {
		maybeFailForTesting = old
	}
}
