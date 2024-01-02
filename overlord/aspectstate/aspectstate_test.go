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

package aspectstate_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/aspectstate"
	"github.com/snapcore/snapd/overlord/state"
)

type aspectTestSuite struct {
	state *state.State
}

var _ = Suite(&aspectTestSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *aspectTestSuite) SetUpTest(_ *C) {
	s.state = overlord.Mock().State()
}

func (s *aspectTestSuite) TestGetAspect(c *C) {
	databag := aspects.NewJSONDataBag()
	err := databag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	res, err := aspectstate.GetAspect(databag, "system", "network", "wifi-setup", "ssid")
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *aspectTestSuite) TestGetNotFound(c *C) {
	databag := aspects.NewJSONDataBag()

	res, err := aspectstate.GetAspect(databag, "system", "network", "other-aspect", "ssid")
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find value for "ssid" in aspect system/network/other-aspect: aspect not found`)
	c.Check(res, IsNil)

	res, err = aspectstate.GetAspect(databag, "system", "network", "wifi-setup", "ssid")
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find value for "ssid" in aspect system/network/wifi-setup: matching rules don't map to any values`)
	c.Check(res, IsNil)

	res, err = aspectstate.GetAspect(databag, "system", "network", "wifi-setup", "other-field")
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find value for "other-field" in aspect system/network/wifi-setup: no matching read rule`)
	c.Check(res, IsNil)
}

func (s *aspectTestSuite) TestSetAspect(c *C) {
	databag := aspects.NewJSONDataBag()
	err := aspectstate.SetAspect(databag, "system", "network", "wifi-setup", "ssid", "foo")
	c.Assert(err, IsNil)

	val, err := databag.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "foo")
}

func (s *aspectTestSuite) TestSetNotFound(c *C) {
	databag := aspects.NewJSONDataBag()
	err := aspectstate.SetAspect(databag, "system", "network", "wifi-setup", "foo", "bar")
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find value for "foo" in aspect system/network/wifi-setup: no matching write rule`)

	err = aspectstate.SetAspect(databag, "system", "network", "other-aspect", "foo", "bar")
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find value for "foo" in aspect system/network/other-aspect: aspect not found`)
}

func (s *aspectTestSuite) TestSetAccessError(c *C) {
	databag := aspects.NewJSONDataBag()
	err := aspectstate.SetAspect(databag, "system", "network", "wifi-setup", "status", "foo")
	c.Assert(err, ErrorMatches, `cannot write field "status": only supports read access`)
}

func (s *aspectTestSuite) TestUnsetAspect(c *C) {
	databag := aspects.NewJSONDataBag()
	err := aspectstate.SetAspect(databag, "system", "network", "wifi-setup", "ssid", "foo")
	c.Assert(err, IsNil)

	err = aspectstate.SetAspect(databag, "system", "network", "wifi-setup", "ssid", nil)
	c.Assert(err, IsNil)

	val, err := databag.Get("wifi.ssid")
	c.Assert(err, FitsTypeOf, aspects.PathError(""))
	c.Assert(val, Equals, nil)
}

func (s *aspectTestSuite) TestNewTransactionExistingState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := aspects.NewJSONDataBag()
	err := bag.Set("foo", "bar")
	c.Assert(err, IsNil)
	databags := map[string]map[string]aspects.JSONDataBag{
		"system": {"network": bag},
	}
	s.state.Set("aspect-databags", databags)

	tx, err := aspectstate.NewTransaction(s.state, "system", "network")
	c.Assert(err, IsNil)

	value, err := tx.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	err = tx.Set("foo", "baz")
	c.Assert(err, IsNil)

	err = tx.Commit()
	c.Assert(err, IsNil)

	err = s.state.Get("aspect-databags", &databags)
	c.Assert(err, IsNil)
	value, err = databags["system"]["network"].Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *aspectTestSuite) TestNewTransactionNoState(c *C) {
	type testcase struct {
		state map[string]map[string]aspects.JSONDataBag
	}

	testcases := []testcase{
		{
			state: map[string]map[string]aspects.JSONDataBag{
				"system": {"network": nil},
			},
		},
		{
			state: map[string]map[string]aspects.JSONDataBag{
				"system": nil,
			},
		},
		{
			state: map[string]map[string]aspects.JSONDataBag{},
		},
		{
			state: nil,
		},
	}

	s.state.Lock()
	defer s.state.Unlock()
	for _, tc := range testcases {
		s.state.Set("aspect-databags", tc.state)

		tx, err := aspectstate.NewTransaction(s.state, "system", "network")
		c.Assert(err, IsNil)

		err = tx.Set("foo", "bar")
		c.Assert(err, IsNil)

		err = tx.Commit()
		c.Assert(err, IsNil)

		var databags map[string]map[string]aspects.JSONDataBag
		err = s.state.Get("aspect-databags", &databags)
		c.Assert(err, IsNil)

		value, err := databags["system"]["network"].Get("foo")
		c.Assert(err, IsNil)
		c.Assert(value, Equals, "bar")
	}
}
