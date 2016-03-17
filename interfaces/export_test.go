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

package interfaces

import (
	"github.com/ubuntu-core/snappy/testutil"
)

// MockActiveSnapMetaData replaces the function used to determine version and origin of a given snap.
func MockActiveSnapMetaData(test *testutil.BaseTest, fn func(string) (string, string, []string, error)) {
	orig := ActiveSnapMetaData
	ActiveSnapMetaData = fn
	test.AddCleanup(func() {
		ActiveSnapMetaData = orig
	})
}

// MockSecCompHeader replaces the real seccomp header blob
func MockSecCompHeader(test *testutil.BaseTest, header []byte) {
	orig := secCompHeader
	secCompHeader = header
	test.AddCleanup(func() {
		secCompHeader = orig
	})
}

// MockAppArmorHeader replaces the real apparmor header blob
func MockAppArmorHeader(test *testutil.BaseTest, header []byte) {
	orig := appArmorHeader
	appArmorHeader = header
	test.AddCleanup(func() {
		appArmorHeader = orig
	})
}
