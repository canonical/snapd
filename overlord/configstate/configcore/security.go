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

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/overlord/configstate/config"
)

func init() {
	// 'required-publisher-validations' restricts snaps to specific publisher validation statuses
	supportedConfigurations["core.system.security.required-publisher-validations"] = true
}

func validateSecuritySettings(tr RunTransaction) error {
	var validationsStr string
	err := tr.Get("core", "system.security.required-publisher-validations", &validationsStr)
	if err != nil && !config.IsNoOption(err) {
		return err
	}

	if validationsStr != "" {
		validations := strings.Split(validationsStr, ",")
		for _, v := range validations {
			v = strings.TrimSpace(v)
			if v != "verified" && v != "starred" && v != "certified" {
				return fmt.Errorf("unsupported publisher validation: %q", v)
			}
		}
	}
	return nil
}
