// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap_test

import (
	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/snap"
)

type restartcondSuite struct{}

var _ = Suite(&restartcondSuite{})

func (*restartcondSuite) TestRestartCondUnmarshal(c *C) {
	for name, cond := range snap.RestartMap {
		bs := []byte(name)
		var rc snap.RestartCondition

		c.Check(yaml.Unmarshal(bs, &rc), IsNil)
		c.Check(rc, Equals, cond, Commentf(name))
	}
}

func (restartcondSuite) TestRestartCondString(c *C) {
	for name, cond := range snap.RestartMap {
		if name == "never" {
			c.Check(cond.String(), Equals, "no")
		} else {
			c.Check(cond.String(), Equals, name, Commentf(name))
		}
	}
}
