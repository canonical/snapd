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

package registrystate_test

import (
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/registrystate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
	"github.com/snapcore/snapd/snap"
)

type registryTestSuite struct {
	state *state.State

	devAccID string
}

var _ = Suite(&registryTestSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *registryTestSuite) SetUpTest(c *C) {
	s.state = overlord.Mock().State()

	s.state.Lock()
	defer s.state.Unlock()

	storeSigning := assertstest.NewStoreStack("can0nical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
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
		"views": map[string]interface{}{
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

	as, err := signingDB.Sign(asserts.RegistryType, headers, body, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(s.state, as), IsNil)

	s.devAccID = devAccKey.AccountID()
}

func (s *registryTestSuite) TestGetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	databag := registry.NewJSONDataBag()
	err := databag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)
	s.state.Set("registry-databags", map[string]map[string]registry.JSONDataBag{s.devAccID: {"network": databag}})

	res, err := registrystate.GetViaView(s.state, s.devAccID, "network", "wifi-setup", []string{"ssid"})
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *registryTestSuite) TestGetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	res, err := registrystate.GetViaView(s.state, s.devAccID, "network", "other-view", []string{"ssid"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" in registry view %s/network/other-view: not found`, s.devAccID))
	c.Check(res, IsNil)

	res, err = registrystate.GetViaView(s.state, s.devAccID, "network", "wifi-setup", []string{"ssid"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" in registry view %s/network/wifi-setup: matching rules don't map to any values`, s.devAccID))
	c.Check(res, IsNil)

	res, err = registrystate.GetViaView(s.state, s.devAccID, "network", "wifi-setup", []string{"other-field"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "other-field" in registry view %s/network/wifi-setup: no matching read rule`, s.devAccID))
	c.Check(res, IsNil)
}

func (s *registryTestSuite) TestSetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := registrystate.SetViaView(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": "foo"})
	c.Assert(err, IsNil)

	var databags map[string]map[string]registry.JSONDataBag
	err = s.state.Get("registry-databags", &databags)
	c.Assert(err, IsNil)

	val, err := databags[s.devAccID]["network"].Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "foo")
}

func (s *registryTestSuite) TestSetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := registrystate.SetViaView(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"foo": "bar"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" in registry view %s/network/wifi-setup: no matching write rule`, s.devAccID))

	err = registrystate.SetViaView(s.state, s.devAccID, "network", "other-view", map[string]interface{}{"foo": "bar"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" in registry view %s/network/other-view: not found`, s.devAccID))
}

func (s *registryTestSuite) TestUnsetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	databag := registry.NewJSONDataBag()
	err := registrystate.SetViaView(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": "foo"})
	c.Assert(err, IsNil)

	err = registrystate.SetViaView(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": nil})
	c.Assert(err, IsNil)

	val, err := databag.Get("wifi.ssid")
	c.Assert(err, FitsTypeOf, registry.PathError(""))
	c.Assert(val, Equals, nil)
}

func (s *registryTestSuite) TestRegistrystateSetWithExistingState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := registry.NewJSONDataBag()
	err := bag.Set("wifi.ssid", "bar")
	c.Assert(err, IsNil)
	databags := map[string]map[string]registry.JSONDataBag{
		s.devAccID: {"network": bag},
	}

	s.state.Set("registry-databags", databags)

	results, err := registrystate.GetViaView(s.state, s.devAccID, "network", "wifi-setup", []string{"ssid"})
	c.Assert(err, IsNil)
	resultsMap, ok := results.(map[string]interface{})
	c.Assert(ok, Equals, true)
	c.Assert(resultsMap["ssid"], Equals, "bar")

	err = registrystate.SetViaView(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": "baz"})
	c.Assert(err, IsNil)

	err = s.state.Get("registry-databags", &databags)
	c.Assert(err, IsNil)
	value, err := databags[s.devAccID]["network"].Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *registryTestSuite) TestRegistrystateSetWithNoState(c *C) {
	type testcase struct {
		state map[string]map[string]registry.JSONDataBag
	}

	testcases := []testcase{
		{
			state: map[string]map[string]registry.JSONDataBag{
				s.devAccID: {"network": nil},
			},
		},
		{
			state: map[string]map[string]registry.JSONDataBag{
				s.devAccID: nil,
			},
		},
		{
			state: map[string]map[string]registry.JSONDataBag{},
		},
		{
			state: nil,
		},
	}

	s.state.Lock()
	defer s.state.Unlock()
	for _, tc := range testcases {
		s.state.Set("registry-databags", tc.state)

		err := registrystate.SetViaView(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{"ssid": "bar"})
		c.Assert(err, IsNil)

		var databags map[string]map[string]registry.JSONDataBag
		err = s.state.Get("registry-databags", &databags)
		c.Assert(err, IsNil)

		value, err := databags[s.devAccID]["network"].Get("wifi.ssid")
		c.Assert(err, IsNil)
		c.Assert(value, Equals, "bar")
	}
}

func (s *registryTestSuite) TestRegistrystateGetEntireView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := registrystate.SetViaView(s.state, s.devAccID, "network", "wifi-setup", map[string]interface{}{
		"ssids":    []interface{}{"foo", "bar"},
		"password": "pass",
		"private": map[string]interface{}{
			"a": 1,
			"b": 2,
		},
	})
	c.Assert(err, IsNil)

	res, err := registrystate.GetViaView(s.state, s.devAccID, "network", "wifi-setup", nil)
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]interface{}{
		"ssids": []interface{}{"foo", "bar"},
		"private": map[string]interface{}{
			"a": float64(1),
			"b": float64(2),
		},
	})
}

func (s *registryTestSuite) TestRegistryTransaction(c *C) {
	mkRegistry := func(account, name string) *registry.Registry {
		reg, err := registry.New(account, name, map[string]interface{}{
			"bar": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "foo", "storage": "foo"},
				},
			},
		}, registry.NewJSONSchema())
		c.Assert(err, IsNil)
		return reg
	}

	s.state.Lock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	s.state.Unlock()
	mockHandler := hooktest.NewMockHandler()

	type testcase struct {
		acc1, acc2 string
		reg1, reg2 string
		equals     bool
	}

	tcs := []testcase{
		{
			// same transaction
			acc1: "acc-1", reg1: "reg-1",
			acc2: "acc-1", reg2: "reg-1",
			equals: true,
		},
		{
			// different registry name, different transaction
			acc1: "acc-1", reg1: "reg-1",
			acc2: "acc-1", reg2: "reg-2",
		},
		{
			// different account, different transaction
			acc1: "acc-1", reg1: "reg-1",
			acc2: "acc-2", reg2: "reg-1",
		},
		{
			// both different, different transaction
			acc1: "acc-1", reg1: "reg-1",
			acc2: "acc-2", reg2: "reg-2",
		},
	}

	for _, tc := range tcs {
		ctx, err := hookstate.NewContext(task, task.State(), setup, mockHandler, "")
		c.Assert(err, IsNil)
		ctx.Lock()

		reg1 := mkRegistry(tc.acc1, tc.reg1)
		reg2 := mkRegistry(tc.acc2, tc.reg2)

		tx1, err := registrystate.RegistryTransaction(ctx, reg1)
		c.Assert(err, IsNil)

		tx2, err := registrystate.RegistryTransaction(ctx, reg2)
		c.Assert(err, IsNil)

		if tc.equals {
			c.Assert(tx1, Equals, tx2)
		} else {
			c.Assert(tx1, Not(Equals), tx2)
		}
		ctx.Unlock()
	}
}
