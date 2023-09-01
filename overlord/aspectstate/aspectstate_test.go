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
	"encoding/json"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/aspectstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
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

	var res interface{}
	err = aspectstate.GetAspect(databag, "system", "network", "wifi-setup", "ssid", &res)
	c.Assert(err, IsNil)
	c.Assert(res, Equals, "foo")
}

func (s *aspectTestSuite) TestGetNotFound(c *C) {
	databag := aspects.NewJSONDataBag()

	var res interface{}
	err := aspectstate.GetAspect(databag, "system", "network", "other-aspect", "ssid", &res)
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find field "ssid" of aspect system/network/other-aspect: aspect not found`)
	c.Check(res, IsNil)

	err = aspectstate.GetAspect(databag, "system", "network", "wifi-setup", "ssid", &res)
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find field "ssid" of aspect system/network/wifi-setup: no value was found under path "wifi"`)
	c.Check(res, IsNil)

	err = aspectstate.GetAspect(databag, "system", "network", "wifi-setup", "other-field", &res)
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find field "other-field" of aspect system/network/wifi-setup: field not found`)
	c.Check(res, IsNil)

}

func (s *aspectTestSuite) TestSetAspect(c *C) {
	databag := aspects.NewJSONDataBag()
	err := aspectstate.SetAspect(databag, "system", "network", "wifi-setup", "ssid", "foo")
	c.Assert(err, IsNil)

	var val string
	err = databag.Get("wifi.ssid", &val)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")
}

func (s *aspectTestSuite) TestSetNotFound(c *C) {
	databag := aspects.NewJSONDataBag()
	err := aspectstate.SetAspect(databag, "system", "network", "wifi-setup", "foo", "bar")
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find field "foo" of aspect system/network/wifi-setup: field not found`)

	err = aspectstate.SetAspect(databag, "system", "network", "other-aspect", "foo", "bar")
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find field "foo" of aspect system/network/other-aspect: aspect not found`)
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

	var val string
	err = databag.Get("wifi.ssid", &val)
	c.Assert(err, FitsTypeOf, aspects.PathNotFoundError(""))
	c.Assert(val, Equals, "")
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

	var value interface{}
	err = tx.Get("foo", &value)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	err = tx.Set("foo", "baz")
	c.Assert(err, IsNil)

	err = tx.Commit()
	c.Assert(err, IsNil)

	err = s.state.Get("aspect-databags", &databags)
	c.Assert(err, IsNil)
	err = databags["system"]["network"].Get("foo", &value)
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

		var value interface{}
		err = databags["system"]["network"].Get("foo", &value)
		c.Assert(err, IsNil)
		c.Assert(value, Equals, "bar")
	}
}

type filterSampleSuite struct {
	state *state.State
	bag   aspects.JSONDataBag
}

var _ = Suite(&filterSampleSuite{})

func (s *filterSampleSuite) SetUpTest(c *C) {
	var raw map[string]json.RawMessage
	err := json.Unmarshal([]byte(`{
		"snaps": {
			"test-snapd-sh": {
				"name":   "test-snapd-sh",
				"status": "installed"
			},
			"core20": {
				"name":   "core20",
				"status": "active"
			},
			"snapcraft": {
				"name":   "snapcraft",
				"status": "active"
			},
			"firefox": {
				"name":   "firefox",
				"status": "active"
			},
			"snapd": {
				"name":   "snapd",
				"status": "active"
			},
			"vlc": {
				"name":   "vlc",
				"status": "inactive"
			},
			"spotify": {
				"name":   "spotify",
				"status": "failed"
			},
			"discord": {
				"name":   "discord",
				"status": "active"
			},
			"shellcheck": {
				"name":   "shellcheck",
				"status": "active"
			},
			"htop": {
				"name":   "htop",
				"status": "inactive"
			}
		}
	}`), &raw)
	c.Assert(err, IsNil)

	s.state = overlord.Mock().State()
	s.bag = aspects.JSONDataBag(raw)
}

func (s *filterSampleSuite) TestQueryNoFilters(c *C) {
	res, err := aspectstate.QueryAspect(s.bag, "acc", "bundle", "asp", "snaps", "")
	c.Assert(err, IsNil)
	// returns all snaps
	c.Assert(res, HasLen, 10)
	// TODO: right now the result is a list of encoded maps, Samuele wants the result to be
	// decoded into maps
	c.Assert(res[0], FitsTypeOf, map[string]interface{}{})
}

func (s *filterSampleSuite) TestQueryFilterNameWithParameter(c *C) {
	res, err := aspectstate.QueryAspect(s.bag, "acc", "bundle", "asp", "snaps", "name=firefox")
	c.Assert(err, IsNil)
	c.Assert(res, HasLen, 1)
	c.Assert(res[0], FitsTypeOf, map[string]interface{}{})

	firefoxSnap := res[0].(map[string]interface{})
	c.Check(firefoxSnap["name"], Equals, "firefox")
	c.Check(firefoxSnap["status"], Equals, "active")
}

func (s *filterSampleSuite) TestQueryFilterNameWithRequest(c *C) {
	res, err := aspectstate.QueryAspect(s.bag, "acc", "bundle", "asp", "snaps.firefox", "")
	c.Assert(err, IsNil)
	c.Assert(res, HasLen, 1)
	c.Assert(res[0], FitsTypeOf, map[string]interface{}{})

	firefoxSnap := res[0].(map[string]interface{})
	c.Check(firefoxSnap["name"], Equals, "firefox")
	c.Check(firefoxSnap["status"], Equals, "active")
}

func (s *filterSampleSuite) TestQueryFilterStatus(c *C) {
	results, err := aspectstate.QueryAspect(s.bag, "acc", "bundle", "asp", "snaps", "status=active")
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 6)

	expectedSnaps := []string{"firefox", "shellcheck", "snapd", "snapcraft", "discord", "core20"}
	seenSnaps := make(map[string]struct{})
	for _, res := range results {
		snap := res.(map[string]json.RawMessage)
		name := parseString(c, snap["name"])
		c.Assert(strutil.ListContains(expectedSnaps, name), Equals, true, Commentf("%v doesn't contain %s", expectedSnaps, name))
		seenSnaps[name] = struct{}{}

		status := parseString(c, snap["status"])
		c.Assert(status, Equals, "active")
	}

	c.Assert(seenSnaps, HasLen, 6)
}

func parseString(c *C, raw json.RawMessage) string {
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		c.Fatal(err, Commentf("expected value to be string"))
	}

	return str
}
