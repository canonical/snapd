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

type aspectstateTestSuite struct {
	state *state.State
}

var _ = Suite(&aspectstateTestSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *aspectstateTestSuite) SetUpTest(_ *C) {
	s.state = overlord.Mock().State()
	aspectstate.ClearHijackedAspects()
}

func (s *aspectstateTestSuite) TestGetAspect(c *C) {
	databag := aspects.NewJSONDataBag()
	err := databag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("aspect-databags", map[string]map[string]aspects.JSONDataBag{
		"system": {"network": databag},
	})
	s.state.Unlock()

	var res interface{}
	err = aspectstate.Get(s.state, "system", "network", "wifi-setup", "ssid", &res)
	c.Assert(err, IsNil)
	c.Assert(res, Equals, "foo")
}

func (s *aspectstateTestSuite) TestGetAspectAssertionNotFound(c *C) {
	s.state.Lock()
	s.state.Set("aspect-databags", map[string]map[string]aspects.JSONDataBag{
		"system": {"network": aspects.NewJSONDataBag()},
	})
	s.state.Unlock()

	var res interface{}
	err := aspectstate.Get(s.state, "unknown", "network", "wifi-setup", "ssid", &res)
	c.Assert(err, FitsTypeOf, &aspects.AspectNotFoundError{})
	c.Assert(err, ErrorMatches, `aspect unknown/network/wifi-setup not found: cannot find aspect assertion unknown/network`)
	c.Check(res, IsNil)
}

func (s *aspectstateTestSuite) TestGetDataBagNotFound(c *C) {
	var res interface{}
	err := aspectstate.Get(s.state, "system", "network", "wifi-setup", "ssid", &res)
	c.Assert(err, FitsTypeOf, &aspects.AspectNotFoundError{})
	c.Assert(err, ErrorMatches, `aspect system/network/wifi-setup not found`)
	c.Check(res, IsNil)

	s.state.Lock()
	s.state.Set("aspect-databags", map[string]map[string]aspects.JSONDataBag{
		"system": {"network": aspects.NewJSONDataBag()},
	})
	s.state.Unlock()

	err = aspectstate.Get(s.state, "system", "network", "other-aspect", "ssid", &res)
	c.Assert(err, FitsTypeOf, &aspects.AspectNotFoundError{})
	c.Assert(err, ErrorMatches, `aspect system/network/other-aspect not found`)
	c.Check(res, IsNil)

	err = aspectstate.Get(s.state, "system", "network", "wifi-setup", "ssid", &res)
	c.Assert(err, FitsTypeOf, &aspects.FieldNotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get field "ssid": no value was found under "wifi"`)
	c.Check(res, IsNil)
}

func (s *aspectstateTestSuite) TestSetAspect(c *C) {
	err := aspectstate.Set(s.state, "system", "network", "wifi-setup", "ssid", "foo")
	c.Assert(err, IsNil)

	var databags map[string]map[string]aspects.JSONDataBag
	s.state.Lock()
	err = s.state.Get("aspect-databags", &databags)
	s.state.Unlock()
	c.Assert(err, IsNil)

	databag := databags["system"]["network"]
	c.Assert(databag, NotNil)

	var val string
	err = databag.Get("wifi.ssid", &val)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")
}

func (s *aspectstateTestSuite) TestSetAspectAssertionNotFound(c *C) {
	err := aspectstate.Set(s.state, "other-system", "network", "wifi-setup", "foo", "bar")
	c.Assert(err, FitsTypeOf, &aspects.AspectNotFoundError{})
	err = aspectstate.Set(s.state, "system", "other-bundle", "wifi-setup", "foo", "bar")
	c.Assert(err, FitsTypeOf, &aspects.AspectNotFoundError{})
	err = aspectstate.Set(s.state, "system", "network", "other-aspect", "foo", "bar")
	c.Assert(err, FitsTypeOf, &aspects.AspectNotFoundError{})
}

func (s *aspectstateTestSuite) TestSetAccessError(c *C) {
	err := aspectstate.Set(s.state, "system", "network", "wifi-setup", "status", "foo")
	c.Assert(err, ErrorMatches, `cannot write field "status": only supports read access`)
}

func (s *aspectstateTestSuite) TestUnsetAspect(c *C) {
	err := aspectstate.Set(s.state, "system", "network", "wifi-setup", "ssid", "foo")
	c.Assert(err, IsNil)

	err = aspectstate.Set(s.state, "system", "network", "wifi-setup", "ssid", nil)
	c.Assert(err, IsNil)

	var databags map[string]map[string]aspects.JSONDataBag
	s.state.Lock()
	err = s.state.Get("aspect-databags", &databags)
	s.state.Unlock()
	c.Assert(err, IsNil)

	databag := databags["system"]["network"]
	c.Assert(databag, NotNil)

	var val string
	err = databag.Get("wifi.ssid", &val)
	c.Assert(err, FitsTypeOf, &aspects.FieldNotFoundError{})
	c.Assert(val, Equals, "")
}

type hijacker struct {
	c *C
	aspectstate.BaseHijacker
}

func (s *hijacker) Get(path string, value interface{}) error {
	s.c.Check(path, Equals, "wifi.ssid")
	*value.(*interface{}) = "hijacked"
	return nil
}

func (s *hijacker) Set(path string, value interface{}) error {
	s.c.Check(path, Equals, "wifi.ssid")
	s.c.Check(value, Equals, "foo")
	return nil
}

func (s *aspectstateTestSuite) TestHijackAspect(c *C) {
	aspectstate.Hijack("system", "network", "wifi-setup", &hijacker{c: c})

	var val interface{}
	err := aspectstate.Get(s.state, "system", "network", "wifi-setup", "ssid", &val)
	c.Assert(err, IsNil)
	c.Check(val, Equals, "hijacked")

	err = aspectstate.Set(s.state, "system", "network", "wifi-setup", "ssid", "foo")
	c.Assert(err, IsNil)

	err = aspectstate.Get(s.state, "system", "network", "wifi-setup", "ssid", &val)
	c.Assert(err, IsNil)
	c.Check(val, Equals, "hijacked")
}
