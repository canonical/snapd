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

type diskSpaceSuite struct {
	configcoreSuite
}

var _ = Suite(&diskSpaceSuite{})

func (s *diskSpaceSuite) TestConfigureDiskSpaceReservation(c *C) {
	for _, tc := range []struct {
		value any
		err   string
	}{
		{value: ""},
		{value: "0B"},
		{value: "5242880B"},
		{value: "5MB"},
		{value: "1GB"},
		{value: "5", err: `cannot parse "5": need a number with a unit as input`},
		{value: "-1B", err: `cannot parse "-1B": size cannot be negative`},
		{value: "bad", err: `cannot parse "bad": no numerical prefix`},
		{value: "5MiB", err: `cannot parse "5MiB": try 'kB' or 'MB'`},
	} {
		err := configcore.Run(classicDev, &mockConf{
			state: s.state,
			changes: map[string]any{
				"system.disk-space-reservation": tc.value,
			},
		})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}
