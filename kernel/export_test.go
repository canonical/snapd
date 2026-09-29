// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package kernel

import (
	"os"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

// MockEnsureInterval sets the overlord ensure interval for tests.
func MockOsSymlink(newSymlink func(string, string) error) (restore func()) {
	old := osSymlink
	osSymlink = newSymlink
	return func() { osSymlink = old }
}

// MockAtomicWriteFile mocks the osutil.AtomicWriteFile wrapper used by
// writeDriversTreeMeta, so tests can simulate a marker-write failure.
func MockAtomicWriteFile(f func(string, []byte, os.FileMode, osutil.AtomicWriteFlags) error) (restore func()) {
	return testutil.Mock(&atomicWriteFile, f)
}

var WriteDriversTreeMeta = writeDriversTreeMeta

var ReadDriversTreeMeta = readDriversTreeMeta

func KernelDriversTreeGeneratorVersion() int {
	return kernelDriversTreeGeneratorVersion
}

// MockKernelDriversTreeGeneratorVersion overrides the generator version
// constant for testing (e.g. to simulate a revert scenario where the
// on-disk marker records a newer version than the running code).
func MockKernelDriversTreeGeneratorVersion(v int) (restore func()) {
	return testutil.Mock(&kernelDriversTreeGeneratorVersion, v)
}
