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

// MockDoSync mocks the syscall.Sync wrapper for tests.
func MockDoSync(newSync func()) (restore func()) {
	old := doSync
	doSync = newSync
	return func() { doSync = old }
}

// MockAtomicWriteFile mocks the osutil.AtomicWriteFile wrapper used by
// writeDriversTreeMeta, so tests can simulate a marker-write failure.
func MockAtomicWriteFile(f func(string, []byte, os.FileMode, osutil.AtomicWriteFlags) error) (restore func()) {
	return testutil.Mock(&atomicWriteFile, f)
}

// MockAtomicSymlink mocks the osutil.AtomicSymlink wrapper used by
// syncFirmwareTopLevelSymlinks, so tests can simulate a failure for a
// specific target/link pair without needing to actually trigger it at the
// filesystem level.
func MockAtomicSymlink(f func(target, linkPath string) error) (restore func()) {
	return testutil.Mock(&atomicSymlink, f)
}

// WriteDriversTreeMeta is exported for testing.
func WriteDriversTreeMeta(destDir string) error {
	return writeDriversTreeMeta(destDir)
}

// ReadDriversTreeGeneratorVersion is exported for testing.
func ReadDriversTreeGeneratorVersion(destDir string) (int, error) {
	meta, err := readDriversTreeGeneratorMeta(destDir)
	if err != nil {
		return 0, err
	}
	return meta.GeneratorVersion, nil
}

// KernelDriversTreeGeneratorVersion returns the current generator version
// constant, exported for testing.
func KernelDriversTreeGeneratorVersion() int {
	return kernelDriversTreeGeneratorVersion
}

// MockKernelDriversTreeGeneratorVersion overrides the generator version
// constant for testing (e.g. to simulate a revert scenario where the
// on-disk marker records a newer version than the running code).
func MockKernelDriversTreeGeneratorVersion(v int) (restore func()) {
	old := kernelDriversTreeGeneratorVersion
	kernelDriversTreeGeneratorVersion = v
	return func() { kernelDriversTreeGeneratorVersion = old }
}
