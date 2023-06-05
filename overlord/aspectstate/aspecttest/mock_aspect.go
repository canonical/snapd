// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023 Canonical Ltd
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

package aspecttest

import (
	"fmt"
)

type NotFound struct {
	account string
	bundle  string
}

func (e *NotFound) Error() string {
	return fmt.Sprintf("cannot find aspect assertion %s/%s", e.account, e.bundle)
}

func (e *NotFound) Is(err error) bool {
	_, ok := err.(*NotFound)
	return ok
}

// GetAspectAssertion returns some mocked aspect access patterns.This will
// eventually be replaced by proper aspect assertions.
var GetAspectAssertion = func(account, bundle string) (map[string]interface{}, error) {
	if account != "system" {
		return nil, &NotFound{account: account, bundle: bundle}
	}

	switch bundle {
	case "network":
		return map[string]interface{}{
			"wifi-setup": []map[string]string{
				{"name": "ssids", "path": "wifi.ssids"},
				{"name": "ssid", "path": "wifi.ssid", "access": "read-write"},
				{"name": "password", "path": "wifi.psk", "access": "write"},
				{"name": "status", "path": "wifi.status", "access": "read"},
				{"name": "private.{placeholder}", "path": "wifi.{placeholder}"},
			},
		}, nil
	case "sysctl":
		return map[string]interface{}{
			"sysctl": []map[string]string{
				{"name": "all", "path": "all", "access": "read"},
				{"name": "{placeholder}", "path": "{placeholder}", "access": "read"},
			},
		}, nil
	}

	return nil, &NotFound{account: account, bundle: bundle}
}
