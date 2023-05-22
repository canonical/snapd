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

// MockWifiSetupAspect returns some mocked aspect access patterns for the
// system/network/wifi-setup. This will eventually be replaced by proper
// aspect assertions.
func MockWifiSetupAspect() map[string]interface{} {
	return map[string]interface{}{
		"wifi-setup": []map[string]string{
			{"name": "ssids", "path": "wifi.ssids"},
			{"name": "ssid", "path": "wifi.ssid", "access": "read-write"},
			{"name": "password", "path": "wifi.psk", "access": "write"},
			{"name": "status", "path": "wifi.status", "access": "read"},
			{"name": "private.{placeholder}", "path": "wifi.{placeholder}"},
		},
	}
}
