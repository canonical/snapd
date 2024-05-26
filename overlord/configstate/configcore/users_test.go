// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2021 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type usersSuite struct {
	configcoreSuite
}

var _ = Suite(&usersSuite{})

func (s *usersSuite) TestUsersCreateAutomaticEarly(c *C) {
	patch := map[string]interface{}{
		"users.create.automatic": "false",
	}
	tr := &mockConf{state: s.state}
	mylog.Check(configcore.Early(classicDev, tr, patch))


	c.Check(tr.conf, DeepEquals, map[string]interface{}{
		"users.create.automatic": false,
	})
}

func (s *usersSuite) TestUsersCreateAutomaticInvalid(c *C) {
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{"users.create.automatic": "foo"},
	}))
	c.Assert(err, ErrorMatches, `users.create.automatic can only be set to 'true' or 'false'`)
}

func (s *usersSuite) TestUsersCreateAutomaticConfigure(c *C) {
	tests := []struct {
		value    interface{}
		expected bool
	}{
		{"true", true},
		{"false", false},
		{true, true},
		{false, false},
	}

	for _, t := range tests {
		conf := &mockConf{
			state: s.state,
			conf:  map[string]interface{}{"users.create.automatic": t.value},
		}
		mylog.Check(configcore.Run(classicDev, conf))


		c.Check(conf.conf["users.create.automatic"], Equals, t.expected)
	}
}
