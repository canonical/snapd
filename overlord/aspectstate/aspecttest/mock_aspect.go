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
	"errors"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/overlord/state"
)

// MaybeMockAspect creates a basic aspect, if no aspects exist in the state.
// This is only a helper for testing the ongoing work.
func MaybeMockAspect(st *state.State) error {
	var test map[string]map[string]*aspects.Directory
	if err := st.Get("aspects", &test); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	} else if err == nil {
		// already mocked the assertion; nothing to do
		return nil
	}

	aspectDir, err := aspects.NewAspectDirectory("network", map[string]interface{}{
		"wifi-setup": []map[string]string{
			{"name": "ssids", "path": "wifi.ssids"},
			{"name": "ssid", "path": "wifi.ssid", "access": "read-write"},
			{"name": "password", "path": "wifi.psk", "access": "write"},
			{"name": "status", "path": "wifi.status", "access": "read"},
			{"name": "private.{placeholder}", "path": "wifi.{placeholder}"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	if err != nil {
		return err
	}

	st.Set("aspects", map[string]map[string]*aspects.Directory{
		"system": {
			"network": aspectDir,
		},
	})

	return nil
}
