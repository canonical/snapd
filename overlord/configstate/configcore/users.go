// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2021 Canonical Ltd
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

import (
	"strings"
)

func init() {
	supportedConfigurations["core.users.create.automatic"] = true
}

func earlyUsersSettingsFilter(values, early map[string]interface{}) {
	for key, v := range values {
		if strings.HasPrefix(key, "users.") && supportedConfigurations["core."+key] {
			early[key] = v
		}
	}
}

func validateUsersSettings(tr RunTransaction) error {
	return validateBoolFlag(tr, "users.create.automatic")
}

func handleUserSettings(tr RunTransaction, opts *fsOnlyContext) error {
	output, err := coreCfg(tr, "users.create.automatic")
	if err != nil {
		return nil
	}

	// normalize the value in case
	switch output {
	case "true":
		tr.Set("core", "users.create.automatic", true)
	case "false":
		tr.Set("core", "users.create.automatic", false)
	}

	return nil
}
