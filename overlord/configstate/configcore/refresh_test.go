// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

func (s *refreshSuite) TestConfigureRefreshTimerHappy(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.timer": "8:00~12:00/2",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshTimerRejected(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.timer": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "invalid": "invalid" is not a valid weekday`)
}

func (s *refreshSuite) TestConfigureLegacyRefreshScheduleHappy(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.schedule": "8:00-12:00",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureLegacyRefreshScheduleRejected(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.schedule": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "invalid": not a valid interval`)

	// check that refresh.schedule is verified against legacy parser
	err = configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.schedule": "8:00~12:00/2",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "8:00~12:00": not a valid interval`)
}

func (s *refreshSuite) TestConfigureRefreshHoldHappy(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.hold": "2018-08-18T15:00:00Z",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshHoldInvalid(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.hold": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `refresh\.hold cannot be parsed:.*`)
}

func (s *refreshSuite) TestConfigureRefreshHoldOnMeteredInvalid(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.metered": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `refresh\.metered value "invalid" is invalid`)
}

func (s *refreshSuite) TestConfigureRefreshHoldOnMeteredHappy(c *C) {
	for _, v := range []string{"", "hold", "force"} {
		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"refresh.metered": v,
			},
		})
		c.Assert(err, IsNil)
	}
}

func (s *refreshSuite) TestConfigureRefreshRetainHappy(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.retain": "4",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshRetainUnderRange(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.retain": "1",
		},
	})
	c.Assert(err, ErrorMatches, `retain must be a number between 2 and 20, not "1"`)
}

func (s *refreshSuite) TestConfigureRefreshRetainOverRange(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.retain": "100",
		},
	})
	c.Assert(err, ErrorMatches, `retain must be a number between 2 and 20, not "100"`)
}

func (s *refreshSuite) TestConfigureRefreshRetainInvalid(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.retain": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `retain must be a number between 2 and 20, not "invalid"`)
}
