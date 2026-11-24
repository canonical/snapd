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

package configcore_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type deviceMgmtSuite struct {
	configcoreSuite
}

var _ = Suite(&deviceMgmtSuite{})

func (s *deviceMgmtSuite) TestConfigureRemoteDeviceManagementEnabled(c *C) {
	for _, tc := range []struct {
		value any
		err   string
	}{
		{value: ""},
		{value: true},
		{value: false},
		{value: "true"},
		{value: "false"},
		{value: "banana", err: `remote-device-management.enabled can only be set to 'true' or 'false'`},
	} {
		err := configcore.Run(classicDev, &mockConf{
			state: s.state,
			changes: map[string]any{
				"remote-device-management.enabled": tc.value,
			},
		})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err, Commentf("value %v", tc.value))
		} else {
			c.Check(err, IsNil, Commentf("value %v", tc.value))
		}
	}
}

func (s *deviceMgmtSuite) TestConfigureRemoteDeviceManagementNormalizesValue(c *C) {
	for _, tc := range []struct {
		value    any
		expected bool
	}{
		{"true", true},
		{"false", false},
		{true, true},
		{false, false},
	} {
		conf := &mockConf{
			state:   s.state,
			changes: map[string]any{"remote-device-management.enabled": tc.value},
		}

		err := configcore.Run(classicDev, conf)
		c.Assert(err, IsNil)

		c.Check(conf.conf["remote-device-management.enabled"], Equals, tc.expected, Commentf("value %v", tc.value))
	}
}
