// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"strconv"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type vitalitySuite struct {
	configcoreSuite
}

var _ = Suite(&vitalitySuite{})

func (s *vitalitySuite) TestConfigureVitalityUnhappyName(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "-invalid-snap-name!yf",
		},
	})
	c.Assert(err, ErrorMatches, `cannot set "resilience.vitality-hint": invalid snap name: ".*"`)
}

func (s *vitalitySuite) TestConfigureVitalityHintTooMany(c *C) {
	l := make([]string, 101)
	for i := range l {
		l[i] = strconv.Itoa(i)
	}
	manyStr := strings.Join(l, ",")
	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": manyStr,
		},
	})
	c.Assert(err, ErrorMatches, `cannot set more than 100 snaps in "resilience.vitality-hint": got 101`)
}

func (s *vitalitySuite) TestConfigureVitalityhappyName(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "valid-snapname",
		},
	})
	c.Assert(err, IsNil)
}
