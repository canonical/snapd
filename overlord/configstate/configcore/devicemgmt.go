// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2026 Canonical Ltd
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

package configcore

const remoteDeviceManagementEnabledOpt = "remote-device-management.enabled"

func init() {
	supportedConfigurations["core."+remoteDeviceManagementEnabledOpt] = true
}

func validateRemoteDeviceManagementSettings(tr RunTransaction) error {
	return validateBoolFlag(tr, remoteDeviceManagementEnabledOpt)
}

func handleRemoteDeviceManagementSettings(tr RunTransaction, opts *fsOnlyContext) error {
	value, err := coreCfg(tr, remoteDeviceManagementEnabledOpt)
	if err != nil {
		return err
	}

	// normalize string values so the option is always stored as a boolean
	switch value {
	case "true":
		return tr.Set("core", remoteDeviceManagementEnabledOpt, true)
	case "false":
		return tr.Set("core", remoteDeviceManagementEnabledOpt, false)
	}

	return nil
}
