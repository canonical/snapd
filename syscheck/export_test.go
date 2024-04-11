// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package syscheck

var (
	CheckSquashfsXzMount = checkSquashfsXzMount
	CheckKernelVersion   = checkKernelVersion
	CheckApparmorUsable  = checkApparmorUsable
	CheckWSL             = checkWSL
	CheckCgroup          = checkCgroup

	CheckFuse = firstCheckFuse
)

func Checks() []func() error {
	return checks
}

func MockChecks(mockChecks []func() error) (restore func()) {
	oldChecks := checks
	checks = mockChecks
	return func() {
		checks = oldChecks
	}
}

func MockAppArmorProfilesPath(path string) (restorer func()) {
	old := apparmorProfilesPath
	apparmorProfilesPath = path
	return func() {
		apparmorProfilesPath = old
	}
}

func MockFuseBinary(new string) (restore func()) {
	oldFuseBinary := fuseBinary
	fuseBinary = new
	return func() {
		fuseBinary = oldFuseBinary
	}
}
