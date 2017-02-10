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
	"github.com/snapcore/snapd/testutil"
)

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
func MockTemplate(fakeTemplate []byte) (restore func()) {
	orig := defaultTemplate
	defaultTemplate = fakeTemplate
	return func() { defaultTemplate = orig }
}

// MockClassicTemplate replaces the classic apprmor template.
func MockClassicTemplate(fakeTemplate []byte) (restore func()) {
	orig := classicTemplate
	classicTemplate = fakeTemplate
	return func() { classicTemplate = orig }
}
