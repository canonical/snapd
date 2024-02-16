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

// MockWifiSetupAspect returns some mocked aspect rules for the
// system/network/wifi-setup. This will eventually be replaced by proper
// aspect assertions.
func MockWifiSetupAspect() map[string]interface{} {
	return map[string]interface{}{
		"wifi-setup": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "ssids", "storage": "wifi.ssids"},
				map[string]interface{}{"request": "ssid", "storage": "wifi.ssid", "access": "read-write"},
				map[string]interface{}{"request": "password", "storage": "wifi.psk", "access": "write"},
				map[string]interface{}{"request": "status", "storage": "wifi.status", "access": "read"},
				map[string]interface{}{"request": "private.{placeholder}", "storage": "wifi.{placeholder}"},
			},
		},
	}
}
