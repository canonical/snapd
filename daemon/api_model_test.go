// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

func (s *apiSuite) TestPostRemodelUnhappy(c *check.C) {
	data, err := json.Marshal(postModelData{NewModel: "invalid model"})
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/model", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := postModel(appsCmd, req, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Assert(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, "cannot decode new model assertion: .*")
}

func (s *apiSuite) TestPostRemodel(c *check.C) {
	oldModel := s.brands.Model("my-brand", "my-old-model", modelDefaults)
	newModel := s.brands.Model("my-brand", "my-old-model", modelDefaults, map[string]interface{}{
		"revision": "2",
	})

	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(deviceMgr)
	st := d.overlord.State()
	st.Lock()
	assertstatetest.AddMany(st, s.storeSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	s.mockModel(c, st, oldModel)
	st.Unlock()

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}
	defer func() { ensureStateSoon = func(st *state.State) {} }()

	var devicestateRemodelGotModel *asserts.Model
	devicestateRemodel = func(st *state.State, nm *asserts.Model) (*state.Change, error) {
		devicestateRemodelGotModel = nm
		chg := st.NewChange("remodel", "...")
		return chg, nil
	}

	// create a valid model assertion
	c.Assert(err, check.IsNil)
	modelEncoded := string(asserts.Encode(newModel))
	data, err := json.Marshal(postModelData{NewModel: modelEncoded})
	c.Check(err, check.IsNil)

	// set it and validate that this is what we was passed to
	// devicestateRemodel
	req, err := http.NewRequest("POST", "/v2/model", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := postModel(appsCmd, req, nil).(*resp)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Check(devicestateRemodelGotModel, check.DeepEquals, newModel)

	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Assert(st.Changes(), check.HasLen, 1)
	chg1 := st.Changes()[0]
	c.Assert(chg, check.DeepEquals, chg1)
	c.Assert(chg.Kind(), check.Equals, "remodel")
	c.Assert(chg.Err(), check.IsNil)

	c.Assert(soon, check.Equals, 1)
}

func (s *apiSuite) TestGetModelNoModelAssertion(c *check.C) {

	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(deviceMgr)

	req, err := http.NewRequest("GET", "/v2/model", nil)
	c.Assert(err, check.IsNil)
	response := getModel(appsCmd, req, nil)
	c.Assert(response, check.FitsTypeOf, &resp{})
	rsp := response.(*resp)
	c.Assert(rsp.Status, check.Equals, 404)
	c.Assert(rsp.Result, check.FitsTypeOf, &errorResult{})
	errRes := rsp.Result.(*errorResult)
	c.Assert(errRes.Kind, check.Equals, errorKindAssertionsNotFound)
	c.Assert(errRes.Value, check.Equals, "model")
	c.Assert(errRes.Message, check.Equals, "no model assertion yet")
}

func (s *apiSuite) TestGetModelHasModelAssertion(c *check.C) {
	// make a model assertion
	theModel := s.brands.Model("my-brand", "my-old-model", modelDefaults)

	// model assertion setup
	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(deviceMgr)
	st := d.overlord.State()
	st.Lock()
	assertstatetest.AddMany(st, s.storeSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	s.mockModel(c, st, theModel)
	st.Unlock()

	// make a new get request to the model endpoint
	req, err := http.NewRequest("GET", "/v2/model", nil)
	c.Assert(err, check.IsNil)
	response := getModel(appsCmd, req, nil)

	// check that we get an assertion response
	c.Assert(response, check.FitsTypeOf, &assertResponse{})

	// check that there is only one assertion
	assertions := response.(*assertResponse).assertions
	c.Assert(assertions, check.HasLen, 1)

	// check that one of the assertion keys matches what's in the model we
	// provided
	assert := assertions[0]
	arch := assert.Header("architecture")
	c.Assert(arch, check.FitsTypeOf, "")
	c.Assert(arch.(string), check.Equals, modelDefaults["architecture"])
}

func (s *apiSuite) TestGetModelNoSerialAssertion(c *check.C) {

	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(deviceMgr)

	req, err := http.NewRequest("GET", "/v2/model/serial", nil)
	c.Assert(err, check.IsNil)
	response := getSerial(appsCmd, req, nil)
	c.Assert(response, check.FitsTypeOf, &resp{})
	rsp := response.(*resp)
	c.Assert(rsp.Status, check.Equals, 404)
	c.Assert(rsp.Result, check.FitsTypeOf, &errorResult{})
	errRes := rsp.Result.(*errorResult)
	c.Assert(errRes.Kind, check.Equals, errorKindAssertionsNotFound)
	c.Assert(errRes.Value, check.Equals, "serial")
	c.Assert(errRes.Message, check.Equals, "no serial assertion yet")
}

func (s *apiSuite) TestGetModelHasSerialAssertion(c *check.C) {
	// make a model assertion
	theModel := s.brands.Model("my-brand", "my-old-model", modelDefaults)

	deviceKey, _ := assertstest.GenerateKey(752)

	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, check.IsNil)

	// model assertion setup
	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(deviceMgr)
	st := d.overlord.State()
	st.Lock()
	assertstatetest.AddMany(st, s.storeSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	s.mockModel(c, st, theModel)

	// in case the name of the serial ever changes, just get it state in State
	// currently it's hard-coded to always be serialserial
	var authStateData auth.AuthState
	err = st.Get("auth", &authStateData)
	c.Assert(err, check.IsNil)
	serial, err := s.brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-old-model",
		"serial":              authStateData.Device.Serial,
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	assertstatetest.AddMany(st, serial)

	st.Unlock()

	// make a new get request to the serial endpoint
	req, err := http.NewRequest("GET", "/v2/model/serial", nil)
	c.Assert(err, check.IsNil)
	response := getSerial(appsCmd, req, nil)

	// check that we get an assertion response
	c.Assert(response, check.FitsTypeOf, &assertResponse{})

	// check that there is only one assertion
	assertions := response.(*assertResponse).assertions
	c.Assert(assertions, check.HasLen, 1)

	// check that the device key in the returned assertion matches what we
	// created above
	assert := assertions[0]
	devKey := assert.Header("device-key")
	c.Assert(devKey, check.FitsTypeOf, "")
	c.Assert(devKey.(string), check.Equals, string(encDevKey))
}
