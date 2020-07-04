// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

// Package snapdenv presents common environment (and related) options
// for snapd components.
package snapdenv

import (
	"os"

	"github.com/snapcore/snapd/osutil"
)

var mockTesting *bool

// is this a testing binary? (see withtestkeys.go)
var testingBinary = false

// Testing returns whether snapd components are under testing.
func Testing() bool {
	if mockTesting != nil {
		return *mockTesting
	}
	ok := osutil.GetenvBool("SNAPPY_TESTING")
	if !ok {
		// assume testing if we are a testing binary and the
		// env is not set explicitly to the contrary
		if testingBinary && os.Getenv("SNAPPY_TESTING") == "" {
			return true
		}
	}
	return ok
}

func MockTesting(testing bool) (restore func()) {
	old := mockTesting
	mockTesting = &testing
	return func() {
		mockTesting = old
	}
}

var mockUseStagingStore *bool

// UseStagingStore returns whether snapd compontents should use the staging store.
func UseStagingStore() bool {
	if mockUseStagingStore != nil {
		return *mockUseStagingStore
	}
	return osutil.GetenvBool("SNAPPY_USE_STAGING_STORE")
}

func MockUseStagingStore(useStaging bool) (restore func()) {
	old := mockUseStagingStore
	mockUseStagingStore = &useStaging
	return func() {
		mockUseStagingStore = old
	}
}

var mockPreseeding *bool

// Preseeding returns whether snapd is preseeding, i.e. performing a
// partial first boot updating only filesystem state inside a chroot.
func Preseeding() bool {
	if mockPreseeding != nil {
		return *mockPreseeding
	}
	return osutil.GetenvBool("SNAPD_PRESEED")
}

func MockPreseeding(preseeding bool) (restore func()) {
	old := mockPreseeding
	mockPreseeding = &preseeding
	return func() {
		mockPreseeding = old
	}
}
