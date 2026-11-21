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

type deviceSuite struct {
	configcoreSuite
}

var _ = Suite(&deviceSuite{})

func (s *deviceSuite) TestAllowUnregister(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf:  map[string]any{"device.allow-unregister": true},
	})
	c.Check(err, IsNil)

	err = configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf:  map[string]any{"device.allow-unregister": "yes"},
	})
	c.Check(err, ErrorMatches, `device.allow-unregister can only be set to 'true' or 'false'`)
}
