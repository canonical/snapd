// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

type refreshSuite struct {
	configcoreSuite
}

var _ = Suite(&refreshSuite{})

func (s *refreshSuite) TestConfigureRefreshScheduleHappy(c *C) {
	err := configcore.Run(&mockConf{
		conf: map[string]interface{}{
			"refresh.schedule": "8:00-12:00",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshScheduleRejected(c *C) {
	err := configcore.Run(&mockConf{
		conf: map[string]interface{}{
			"refresh.schedule": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "invalid": not a valid interval`)
}
