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
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/aspectstate"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/state"
)

type aspectTestSuite struct {
	state *state.State

	devAccID string
}

var _ = Suite(&aspectTestSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *aspectTestSuite) SetUpTest(c *C) {
	s.state = overlord.Mock().State()

	s.state.Lock()
	defer s.state.Unlock()

	storeSigning := assertstest.NewStoreStack("can0nical", nil)
	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   storeSigning.Trusted,
	}))

	c.Assert(db.Add(storeSigning.StoreAccountKey("")), IsNil)
	assertstate.ReplaceDB(s.state, db)

	// add developer1's account and account-key assertions
	devAcc := assertstest.NewAccount(storeSigning, "developer1", nil, "")
	c.Assert(storeSigning.Add(devAcc), IsNil)

	devPrivKey, _ := assertstest.GenerateKey(752)
	devAccKey := assertstest.NewAccountKey(storeSigning, devAcc, nil, devPrivKey.PublicKey(), "")

	assertstatetest.AddMany(s.state, storeSigning.StoreAccountKey(""), devAcc, devAccKey)

	signingDB := assertstest.NewSigningDB("developer1", devPrivKey)
	c.Check(signingDB, NotNil)
	c.Assert(storeSigning.Add(devAccKey), IsNil)

	headers := map[string]interface{}{
		"authority-id": devAccKey.AccountID(),
		"account-id":   devAccKey.AccountID(),
		"name":         "network",
		"aspects": map[string]interface{}{
			"wifi-setup": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "ssids", "storage": "wifi.ssids"},
					map[string]interface{}{"request": "ssid", "storage": "wifi.ssid", "access": "read-write"},
					map[string]interface{}{"request": "password", "storage": "wifi.psk", "access": "write"},
					map[string]interface{}{"request": "status", "storage": "wifi.status", "access": "read"},
					map[string]interface{}{"request": "private.{placeholder}", "storage": "private.{placeholder}"},
				},
			},
		},
		"timestamp": "2030-11-06T09:16:26Z",
	}
	body := []byte(`{
  "storage": {
    "schema": {
      "private": {
        "values": "any"
      },
      "wifi": {
        "schema": {
          "psk": "string",
          "ssid": "string",
          "ssids": {
            "type": "array",
            "values": "any"
          },
          "status": "string"
        }
      }
    }
  }
}`)

	as := mylog.Check2(signingDB.Sign(asserts.AspectBundleType, headers, body, ""))

	c.Assert(assertstate.Add(s.state, as), IsNil)

	s.devAccID = devAccKey.AccountID()
}

func (s *aspectTestSuite) TestGetAspect(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	databag := aspects.NewJSONDataBag()
	mylog.Check(databag.Set("wifi.ssid", "foo"))

	s.state.Set("aspect-databags", map[string]map[string]aspects.JSONDataBag{s.devAccID: {"network": databag}})

	res := mylog.Check2(aspectstate.GetAspect(s.state, s.devAccID, "network", "wifi-setup", []string{"ssid"}))

	c.Assert(res, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *aspectTestSuite) TestGetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	res := mylog.Check2(aspectstate.GetAspect(s.state, s.devAccID, "network", "other-aspect", []string{"ssid"}))
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" in aspect %s/network/other-aspect: aspect not found`, s.devAccID))
	c.Check(res, IsNil)

	res = mylog.Check2(aspectstate.GetAspect(s.state, s.devAccID, "network", "wifi-setup", []string{"ssid"}))
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" in aspect %s/network/wifi-setup: matching rules don't map to any values`, s.devAccID))
	c.Check(res, IsNil)

	res = mylog.Check2(aspectstate.GetAspect(s.state, s.devAccID, "network", "wifi-setup", []string{"other-field"}))
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "other-field" in aspect %s/network/wifi-setup: no matching read rule`, s.devAccID))
	c.Check(res, IsNil)
}

func (s *aspectTestSuite) TestSetAspect(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	mylog.Check(aspectstate.SetAspect(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": "foo"}))


	var databags map[string]map[string]aspects.JSONDataBag
	mylog.Check(s.state.Get("aspect-databags", &databags))


	val := mylog.Check2(databags[s.devAccID]["network"].Get("wifi.ssid"))

	c.Assert(val, DeepEquals, "foo")
}

func (s *aspectTestSuite) TestSetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	mylog.Check(aspectstate.SetAspect(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"foo": "bar"}))
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" in aspect %s/network/wifi-setup: no matching write rule`, s.devAccID))
	mylog.Check(aspectstate.SetAspect(s.state, s.devAccID, "network", "other-aspect", map[string]interface{}{"foo": "bar"}))
	c.Assert(err, FitsTypeOf, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" in aspect %s/network/other-aspect: aspect not found`, s.devAccID))
}

func (s *aspectTestSuite) TestUnsetAspect(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	databag := aspects.NewJSONDataBag()
	mylog.Check(aspectstate.SetAspect(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": "foo"}))

	mylog.Check(aspectstate.SetAspect(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": nil}))


	val := mylog.Check2(databag.Get("wifi.ssid"))
	c.Assert(err, FitsTypeOf, aspects.PathError(""))
	c.Assert(val, Equals, nil)
}

func (s *aspectTestSuite) TestAspectstateSetWithExistingState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := aspects.NewJSONDataBag()
	mylog.Check(bag.Set("wifi.ssid", "bar"))

	databags := map[string]map[string]aspects.JSONDataBag{
		s.devAccID: {"network": bag},
	}
	s.state.Set("aspect-databags", databags)

	results := mylog.Check2(aspectstate.GetAspect(s.state, s.devAccID, "network", "wifi-setup", []string{"ssid"}))

	resultsMap, ok := results.(map[string]interface{})
	c.Assert(ok, Equals, true)
	c.Assert(resultsMap["ssid"], Equals, "bar")
	mylog.Check(aspectstate.SetAspect(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": "baz"}))

	mylog.Check(s.state.Get("aspect-databags", &databags))

	value := mylog.Check2(databags[s.devAccID]["network"].Get("wifi.ssid"))

	c.Assert(value, Equals, "baz")
}

func (s *aspectTestSuite) TestAspectstateSetWithNoState(c *C) {
	type testcase struct {
		state map[string]map[string]aspects.JSONDataBag
	}

	testcases := []testcase{
		{
			state: map[string]map[string]aspects.JSONDataBag{
				s.devAccID: {"network": nil},
			},
		},
		{
			state: map[string]map[string]aspects.JSONDataBag{
				s.devAccID: nil,
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
		mylog.Check(aspectstate.SetAspect(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": "bar"}))


		var databags map[string]map[string]aspects.JSONDataBag
		mylog.Check(s.state.Get("aspect-databags", &databags))


		value := mylog.Check2(databags[s.devAccID]["network"].Get("wifi.ssid"))

		c.Assert(value, Equals, "bar")
	}
}

func (s *aspectTestSuite) TestAspectstateGetEntireAspect(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	mylog.Check(aspectstate.SetAspect(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{
		"ssids":    []interface{}{"foo", "bar"},
		"password": "pass",
		"private": map[string]interface{}{
			"a": 1,
			"b": 2,
		},
	}))


	res := mylog.Check2(aspectstate.GetAspect(s.state, s.devAccID, "network", "wifi-setup", nil))

	c.Assert(res, DeepEquals, map[string]interface{}{
		"ssids": []interface{}{"foo", "bar"},
		"private": map[string]interface{}{
			"a": float64(1),
			"b": float64(2),
		},
	})
}
