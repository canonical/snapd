// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type refreshSuite struct {
	configcoreSuite
}

var _ = Suite(&refreshSuite{})

func (s *refreshSuite) TestConfigureRefreshTimerHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.timer": "8:00~12:00/2",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshTimerRejected(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.timer": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "invalid": "invalid" is not a valid weekday`)
}

func (s *refreshSuite) TestConfigureRefreshTimerManagedIgnored(c *C) {
	for _, opt := range []string{"refresh.timer", "refresh.schedule"} {
		cfg := &mockConf{
			state: s.state,
			// invalid value present in the state
			conf: map[string]interface{}{
				opt: "managed",
			},
		}
		s.state.Lock()
		s.state.OkayWarnings(time.Now())
		s.state.Unlock()
		err := configcore.Run(classicDev, cfg)
		c.Assert(err, IsNil)

		s.state.Lock()
		c.Check(cfg.conf[opt], Equals, "managed")
		s.state.Unlock()
	}
}

func (s *refreshSuite) TestConfigureRefreshTimerManagedChangeError(c *C) {
	for _, opt := range []string{"refresh.timer", "refresh.schedule"} {
		cfg := &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				// valid value in the state, should remain intact
				opt: "fri",
			},
			changes: map[string]interface{}{
				opt: "managed",
			},
		}
		err := configcore.Run(classicDev, cfg)
		c.Assert(err, ErrorMatches, `cannot set schedule to managed`)
		// old value still present
		c.Check(cfg.conf[opt], Equals, "fri")
	}
}

func (s *refreshSuite) TestConfigureLegacyRefreshScheduleHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.schedule": "8:00-12:00",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureLegacyRefreshScheduleRejected(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.schedule": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "invalid": not a valid interval`)

	// check that refresh.schedule is verified against legacy parser
	err = configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.schedule": "8:00~12:00/2",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "8:00~12:00": not a valid interval`)
}

func (s *refreshSuite) TestConfigureRefreshHoldHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.hold": "2018-08-18T15:00:00Z",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshHoldForeverHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.hold": "forever",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshHoldInvalid(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.hold": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `refresh\.hold cannot be parsed:.*`)
}

func (s *refreshSuite) TestConfigureRefreshHoldOnMeteredInvalid(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.metered": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `refresh\.metered value "invalid" is invalid`)
}

func (s *refreshSuite) TestConfigureRefreshHoldOnMeteredHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.metered": "hold",
		},
	})
	c.Assert(err, IsNil)

	err = configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.metered": "",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshRetainHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.retain": "4",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshRetainUnderRange(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.retain": "1",
		},
	})
	c.Assert(err, ErrorMatches, `retain must be a number between 2 and 20, not "1"`)
}

func (s *refreshSuite) TestConfigureRefreshRetainOverRange(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.retain": "100",
		},
	})
	c.Assert(err, ErrorMatches, `retain must be a number between 2 and 20, not "100"`)
}

func (s *refreshSuite) TestConfigureRefreshRetainInvalid(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.retain": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `retain must be a number between 2 and 20, not "invalid"`)
}

func (s *refreshSuite) TestConfigureRefreshMaxInhibitionDays(c *C) {
	data := []struct {
		val interface{}
		err string
	}{
		{val: "zzz", err: `max-inhibition-days must be a number between 1 and 21, not "zzz"`},
		{val: "-1", err: `max-inhibition-days must be a number between 1 and 21, not "-1"`},
		{val: -1, err: `max-inhibition-days must be a number between 1 and 21, not "-1"`},
		{val: "23", err: `max-inhibition-days must be a number between 1 and 21, not "23"`},
		{val: 0, err: `max-inhibition-days must be a number between 1 and 21, not "0"`},
		// happy cases
		{val: nil}, // default value (14 days)
		{val: ""},  // default value (14 days)
		{val: 1},
		{val: "1"},
		{val: 14},
	}
	for _, tc := range data {
		err := configcore.Run(classicDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"refresh.max-inhibition-days": tc.val,
			},
		})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}
