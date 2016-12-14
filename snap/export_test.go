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

package snap

import "github.com/snapcore/snapd/release"

var (
	NewHookType = newHookType
)

func ImplicitSlotsForTests() []string {
	result := implicitSlots
	// fuse-support is disabled on trusty due to usage of fuse requiring access to mount.
	// we do not want to widen the apparmor profile defined in fuse-support to support trusty
	// right now.
	if !(release.ReleaseInfo.ID == "ubuntu" && release.ReleaseInfo.VersionID == "14.04") {
		result = append(result, "fuse-support")
	}
	return result
}

func ImplicitClassicSlotsForTests() []string {
	return implicitClassicSlots
}

func MockSupportedHookTypes(hookTypes []*HookType) (restore func()) {
	old := supportedHooks
	supportedHooks = hookTypes
	return func() { supportedHooks = old }
}
