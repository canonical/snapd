// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package apparmor

import (
	"io"
	"os"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

var (
	NumberOfJobsParam  = numberOfJobsParam
	SetupConfCacheDirs = setupConfCacheDirs
)

func MockRuntimeNumCPU(new func() int) (restore func()) {
	old := runtimeNumCPU
	runtimeNumCPU = new
	return func() {
		runtimeNumCPU = old
	}
}

func MockMkdirAll(f func(string, os.FileMode) error) func() {
	r := testutil.Backup(&osMkdirAll)
	osMkdirAll = f
	return r
}

func MockAtomicWrite(f func(string, io.Reader, os.FileMode, osutil.AtomicWriteFlags) error) func() {
	r := testutil.Backup(&osutilAtomicWrite)
	osutilAtomicWrite = f
	return r
}

func MockLoadProfiles(f func([]string, string, AaParserFlags) error) func() {
	r := testutil.Backup(&LoadProfiles)
	LoadProfiles = f
	return r
}

func MockSnapConfineDistroProfilePath(f func() string) func() {
	r := testutil.Backup(&SnapConfineDistroProfilePath)
	SnapConfineDistroProfilePath = f
	return r
}

func MockLoadHomedirs(f func() ([]string, error)) func() {
	r := testutil.Backup(&loadHomedirs)
	loadHomedirs = f
	return r
}

// MockProfilesPath mocks the file read by LoadedProfiles()
func MockProfilesPath(t *testutil.BaseTest, profiles string) {
	profilesPath = profiles
	t.AddCleanup(func() {
		profilesPath = realProfilesPath
	})
}

func MockFsRootPath(path string) (restorer func()) {
	old := rootPath
	rootPath = path
	return func() {
		rootPath = old
	}
}

func MockSnapdAppArmorSupportsReexec(new func() bool) (restore func()) {
	restore = testutil.Backup(&snapdAppArmorSupportsReexec)
	snapdAppArmorSupportsReexec = new
	return restore
}

var (
	ProbeKernelFeatures = probeKernelFeatures
	ProbeParserFeatures = probeParserFeatures

	RequiredKernelFeatures  = requiredKernelFeatures
	RequiredParserFeatures  = requiredParserFeatures
	PreferredKernelFeatures = preferredKernelFeatures
	PreferredParserFeatures = preferredParserFeatures

	SnapdAppArmorSupportsRexecImpl = snapdAppArmorSupportsReexecImpl
	SystemAppArmorLoadsSnapPolicy  = systemAppArmorLoadsSnapPolicy
)

func FreshAppArmorAssessment() {
	appArmorAssessment = &appArmorAssess{appArmorProber: &appArmorProbe{}}
}
