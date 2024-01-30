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
	"os"

	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/wrappers"
)

var (
	AddMountUnit    = addMountUnit
	RemoveMountUnit = removeMountUnit
	RemoveIfEmpty   = removeIfEmpty
)

func MockWrappersAddSnapdSnapServices(f func(s *snap.Info, opts *wrappers.AddSnapdSnapServicesOptions, inter wrappers.Interacter) (*wrappers.Restart, error)) (restore func()) {
	old := wrappersAddSnapdSnapServices
	wrappersAddSnapdSnapServices = f
	return func() {
		wrappersAddSnapdSnapServices = old
	}
}

func MockRemoveIfEmpty(f func(dir string) error) func() {
	old := removeIfEmpty
	removeIfEmpty = f
	return func() {
		removeIfEmpty = old
	}
}

func MockMkdirAllChown(f func(string, os.FileMode, sys.UserID, sys.GroupID) error) func() {
	old := mkdirAllChown
	mkdirAllChown = f
	return func() {
		mkdirAllChown = old
	}
}
