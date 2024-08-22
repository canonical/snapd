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

	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

var (
	AddMountUnit       = addMountUnit
	RemoveMountUnit    = removeMountUnit
	RemoveIfEmpty      = removeIfEmpty
	SnapDataDirs       = snapDataDirs
	SnapCommonDataDirs = snapCommonDataDirs
)

func MockWrappersAddSnapdSnapServices(f func(s *snap.Info, opts *wrappers.AddSnapdSnapServicesOptions, inter wrappers.Interacter) (wrappers.SnapdRestart, error)) (restore func()) {
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

func MockMkdir(f func(string, os.FileMode, *osutil.MkdirOptions) error) func() {
	old := mkdir
	mkdir = f
	return func() {
		mkdir = old
	}
}

func MockKernelEnsureKernelDriversTree(f func(kMntPts kernel.MountPoints, compsMntPts []kernel.ModulesCompMountPoints, destDir string, opts *kernel.KernelDriversTreeOptions) (err error)) func() {
	return testutil.Mock(&kernelEnsureKernelDriversTree, f)
}
