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
	"fmt"
	"io/ioutil"
	"os"

	"github.com/snapcore/snapd/testutil"
)

var (
	IsHomeUsingNFS = isHomeUsingNFS
)

//MockMountInfo mocks content of /proc/self/mountinfo read by isHomeUsingNFS
func MockMountInfo(text string) (restore func()) {
	old := procSelfMountInfo
	f, err := ioutil.TempFile("", "mountinfo")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := ioutil.WriteFile(f.Name(), []byte(text), 0644); err != nil {
		panic(fmt.Errorf("cannot write mock mountinfo file: %s", err))
	}
	procSelfMountInfo = f.Name()
	return func() {
		os.Remove(procSelfMountInfo)
		procSelfMountInfo = old
	}
}

// MockEtcFstab mocks content of /etc/fstab read by isHomeUsingNFS
func MockEtcFstab(text string) (restore func()) {
	old := etcFstab
	f, err := ioutil.TempFile("", "fstab")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := ioutil.WriteFile(f.Name(), []byte(text), 0644); err != nil {
		panic(fmt.Errorf("cannot write mock fstab file: %s", err))
	}
	etcFstab = f.Name()
	return func() {
		if etcFstab == "/etc/fstab" {
			panic("respectfully refusing to remove /etc/fstab")
		}
		os.Remove(etcFstab)
		etcFstab = old
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
