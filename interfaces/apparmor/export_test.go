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

package apparmor

import (
	"os"

	"github.com/snapcore/snapd/testutil"
)

var (
	ChopTree                   = chopTree
	NsProfile                  = nsProfile
	ProfileGlobs               = profileGlobs
	SnapConfineFromSnapProfile = snapConfineFromSnapProfile
	DowngradeConfinement       = downgradeConfinement
	LoadProfiles               = loadProfiles
)

// MockIsHomeUsingNFS mocks the real implementation of osutil.IsHomeUsingNFS
func MockIsHomeUsingNFS(new func() (bool, error)) (restore func()) {
	old := isHomeUsingNFS
	isHomeUsingNFS = new
	return func() {
		isHomeUsingNFS = old
	}
}

// MockIsRootWritableOverlay mocks the real implementation of osutil.IsRootWritableOverlay
func MockIsRootWritableOverlay(new func() (string, error)) (restore func()) {
	old := isRootWritableOverlay
	isRootWritableOverlay = new
	return func() {
		isRootWritableOverlay = old
	}
}

// MockProcSelfExe mocks the location of /proc/self/exe read by setupSnapConfineGeneratedPolicy.
func MockProcSelfExe(symlink string) (restore func()) {
	old := procSelfExe
	procSelfExe = symlink
	return func() {
		os.Remove(procSelfExe)
		procSelfExe = old
	}
}

// MockProfilesPath mocks the file read by LoadedProfiles()
func MockProfilesPath(t *testutil.BaseTest, profiles string) {
	profilesPath = profiles
	t.AddCleanup(func() {
		profilesPath = realProfilesPath
	})
}

// MockTemplate replaces apprmor template.
//
// NOTE: The real apparmor template is long. For testing it is convenient for
// replace it with a shorter snippet.
func MockTemplate(fakeTemplate string) (restore func()) {
	orig := defaultTemplate
	defaultTemplate = fakeTemplate
	return func() { defaultTemplate = orig }
}

// MockClassicTemplate replaces the classic apprmor template.
func MockClassicTemplate(fakeTemplate string) (restore func()) {
	orig := classicTemplate
	classicTemplate = fakeTemplate
	return func() { classicTemplate = orig }
}

// SetSpecScope sets the scope of a given specification
func SetSpecScope(spec *Specification, securityTags []string) (restore func()) {
	return spec.setScope(securityTags)
}

func MockKernelFeatures(f func() []string) (resture func()) {
	old := kernelFeatures
	kernelFeatures = f
	return func() {
		kernelFeatures = old
	}
}
