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

package prompting

import (
	"github.com/snapcore/snapd/testutil"
)

var (
	InterfacePermissionsAvailable = interfacePermissionsAvailable
	InterfaceFilePermissionsMaps  = interfaceFilePermissionsMaps
)

func MockApparmorInterfaceForMetadataTag(f func(tag string) (string, bool)) (restore func()) {
	return testutil.Mock(&apparmorInterfaceForMetadataTag, f)
}

func (pm PermissionMap) ToRulePermissionMap(iface string, at At) (RulePermissionMap, error) {
	return pm.toRulePermissionMap(iface, at)
}

func (pm PermissionMap) PatchRulePermissions(existing RulePermissionMap, iface string, at At) (RulePermissionMap, error) {
	return pm.patchRulePermissions(existing, iface, at)
}

func (pm RulePermissionMap) ValidateForInterface(iface string, at At) (bool, error) {
	return pm.validateForInterface(iface, at)
}
