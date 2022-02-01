// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2021 Canonical Ltd
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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

var modelDefaults = map[string]interface{}{
	"architecture": "amd64",
	"gadget":       "gadget",
	"kernel":       "kernel",
}

var _ = check.Suite(&modelSuite{})

type modelSuite struct {
	apiBaseSuite
}

func (s *modelSuite) TestPostRemodelUnhappy(c *check.C) {
	s.daemon(c)

	s.expectRootAccess()

	data, err := json.Marshal(daemon.PostModelData{NewModel: "invalid model"})
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/model", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, "cannot decode new model assertion: .*")
}

func (s *modelSuite) TestPostRemodel(c *check.C) {
	s.expectRootAccess()

	oldModel := s.Brands.Model("my-brand", "my-old-model", modelDefaults)
	newModel := s.Brands.Model("my-brand", "my-old-model", modelDefaults, map[string]interface{}{
		"revision": "2",
	})

	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(deviceMgr)
	st := d.Overlord().State()
	st.Lock()
	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.Brands.AccountsAndKeys("my-brand")...)
	s.mockModel(st, oldModel)
	st.Unlock()

	soon := 0
	var origEnsureStateSoon func(*state.State)
	origEnsureStateSoon, restore := daemon.MockEnsureStateSoon(func(st *state.State) {
		soon++
		origEnsureStateSoon(st)
	})
	defer restore()

	var devicestateRemodelGotModel *asserts.Model
	defer daemon.MockDevicestateRemodel(func(st *state.State, nm *asserts.Model) (*state.Change, error) {
		devicestateRemodelGotModel = nm
		chg := st.NewChange("remodel", "...")
		return chg, nil
	})()

	// create a valid model assertion
	c.Assert(err, check.IsNil)
	modelEncoded := string(asserts.Encode(newModel))
	data, err := json.Marshal(daemon.PostModelData{NewModel: modelEncoded})
	c.Check(err, check.IsNil)

	// set it and validate that this is what we was passed to
	// devicestateRemodel
	req, err := http.NewRequest("POST", "/v2/model", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := s.asyncReq(c, req, nil)
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

func (s *modelSuite) TestGetModelNoModelAssertion(c *check.C) {

	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(deviceMgr)

	req, err := http.NewRequest("GET", "/v2/model", nil)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 404)
	c.Assert(rspe.Kind, check.Equals, client.ErrorKindAssertionNotFound)
	c.Assert(rspe.Value, check.Equals, "model")
	c.Assert(rspe.Message, check.Equals, "no model assertion yet")
}

func (s *modelSuite) TestGetModelHasModelAssertion(c *check.C) {
	// make a model assertion
	theModel := s.Brands.Model("my-brand", "my-old-model", modelDefaults)

	// model assertion setup
	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(deviceMgr)
	st := d.Overlord().State()
	st.Lock()
	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.Brands.AccountsAndKeys("my-brand")...)
	s.mockModel(st, theModel)
	st.Unlock()

	// make a new get request to the model endpoint
	req, err := http.NewRequest("GET", "/v2/model", nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)

	// check that we get an assertion response
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/x.ubuntu.assertion")

	// check that there is only one assertion
	dec := asserts.NewDecoder(rec.Body)
	m, err := dec.Decode()
	c.Assert(err, check.IsNil)
	_, err = dec.Decode()
	c.Assert(err, check.Equals, io.EOF)

	// check that one of the assertion keys matches what's in the model we
	// provided
	c.Check(m.Type(), check.Equals, asserts.ModelType)
	arch := m.Header("architecture")
	c.Assert(arch, check.FitsTypeOf, "")
	c.Assert(arch.(string), check.Equals, "amd64")
}

func (s *modelSuite) TestGetModelJSONHasModelAssertion(c *check.C) {
	// make a model assertion
	theModel := s.Brands.Model("my-brand", "my-old-model", modelDefaults)

	// model assertion setup
	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(deviceMgr)
	st := d.Overlord().State()
	st.Lock()
	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.Brands.AccountsAndKeys("my-brand")...)
	s.mockModel(st, theModel)
	st.Unlock()

	// make a new get request to the model endpoint with json as true
	req, err := http.NewRequest("GET", "/v2/model?json=true", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	// get the body and try to unmarshal into modelAssertJSON
	c.Assert(rsp.Result, check.FitsTypeOf, daemon.ModelAssertJSON{})

	jsonResponse := rsp.Result.(daemon.ModelAssertJSON)

	// get the architecture key from the headers
	arch, ok := jsonResponse.Headers["architecture"]
	c.Assert(ok, check.Equals, true)

	// ensure that the architecture key is what we set in the model defaults
	c.Assert(arch, check.FitsTypeOf, "")
	c.Assert(arch.(string), check.Equals, "amd64")
}

func (s *modelSuite) TestGetModelNoSerialAssertion(c *check.C) {

	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(deviceMgr)

	req, err := http.NewRequest("GET", "/v2/model/serial", nil)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 404)
	c.Assert(rspe.Kind, check.Equals, client.ErrorKindAssertionNotFound)
	c.Assert(rspe.Value, check.Equals, "serial")
	c.Assert(rspe.Message, check.Equals, "no serial assertion yet")
}

func (s *modelSuite) TestGetModelHasSerialAssertion(c *check.C) {
	// make a model assertion
	theModel := s.Brands.Model("my-brand", "my-old-model", modelDefaults)

	deviceKey, _ := assertstest.GenerateKey(752)

	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, check.IsNil)

	// model assertion setup
	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(deviceMgr)
	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.Brands.AccountsAndKeys("my-brand")...)
	s.mockModel(st, theModel)

	serial, err := s.Brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-old-model",
		"serial":              "serialserial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	assertstatetest.AddMany(st, serial)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-old-model",
		Serial: "serialserial",
	})

	st.Unlock()
	defer st.Lock()

	// make a new get request to the serial endpoint
	req, err := http.NewRequest("GET", "/v2/model/serial", nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)

	// check that we get an assertion response
	c.Check(rec.Code, check.Equals, 200, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/x.ubuntu.assertion")

	// check that there is only one assertion
	dec := asserts.NewDecoder(rec.Body)
	ser, err := dec.Decode()
	c.Assert(err, check.IsNil)
	_, err = dec.Decode()
	c.Assert(err, check.Equals, io.EOF)

	// check that the device key in the returned assertion matches what we
	// created above
	c.Check(ser.Type(), check.Equals, asserts.SerialType)
	devKey := ser.Header("device-key")
	c.Assert(devKey, check.FitsTypeOf, "")
	c.Assert(devKey.(string), check.Equals, string(encDevKey))
}

func (s *modelSuite) TestGetModelJSONHasSerialAssertion(c *check.C) {
	// make a model assertion
	theModel := s.Brands.Model("my-brand", "my-old-model", modelDefaults)

	deviceKey, _ := assertstest.GenerateKey(752)

	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, check.IsNil)

	// model assertion setup
	d := s.daemonWithOverlordMockAndStore()
	hookMgr, err := hookstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.Overlord().State(), hookMgr, d.Overlord().TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.Overlord().AddManager(deviceMgr)
	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.Brands.AccountsAndKeys("my-brand")...)
	s.mockModel(st, theModel)

	serial, err := s.Brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-old-model",
		"serial":              "serialserial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	assertstatetest.AddMany(st, serial)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-old-model",
		Serial: "serialserial",
	})

	st.Unlock()
	defer st.Lock()

	// make a new get request to the model endpoint with json as true
	req, err := http.NewRequest("GET", "/v2/model/serial?json=true", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	// get the body and try to unmarshal into modelAssertJSON
	c.Assert(rsp.Result, check.FitsTypeOf, daemon.ModelAssertJSON{})

	jsonResponse := rsp.Result.(daemon.ModelAssertJSON)

	// get the architecture key from the headers
	devKey, ok := jsonResponse.Headers["device-key"]
	c.Assert(ok, check.Equals, true)

	// check that the device key in the returned assertion matches what we
	// created above
	c.Assert(devKey, check.FitsTypeOf, "")
	c.Assert(devKey.(string), check.Equals, string(encDevKey))
}

func (s *userSuite) TestPostSerialBadAction(c *check.C) {
	buf := bytes.NewBufferString(`{"action":"what"}`)
	req, err := http.NewRequest("POST", "/v2/model/serial", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe, check.DeepEquals, daemon.BadRequest(`unsupported serial action "what"`))
}

func (s *userSuite) TestPostSerialForget(c *check.C) {
	unregister := 0
	defer daemon.MockDevicestateDeviceManagerUnregister(func(mgr *devicestate.DeviceManager, opts *devicestate.UnregisterOptions) error {
		unregister++
		c.Check(mgr, check.NotNil)
		c.Check(opts.NoRegistrationUntilReboot, check.Equals, false)
		return nil
	})()

	buf := bytes.NewBufferString(`{"action":"forget"}`)
	req, err := http.NewRequest("POST", "/v2/model/serial", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Result, check.IsNil)

	c.Check(unregister, check.Equals, 1)
}

func (s *userSuite) TestPostSerialForgetNoRegistrationUntilReboot(c *check.C) {
	unregister := 0
	defer daemon.MockDevicestateDeviceManagerUnregister(func(mgr *devicestate.DeviceManager, opts *devicestate.UnregisterOptions) error {
		unregister++
		c.Check(mgr, check.NotNil)
		c.Check(opts.NoRegistrationUntilReboot, check.Equals, true)
		return nil
	})()

	buf := bytes.NewBufferString(`{"action":"forget", "no-registration-until-reboot": true}`)
	req, err := http.NewRequest("POST", "/v2/model/serial", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Result, check.IsNil)

	c.Check(unregister, check.Equals, 1)
}

func (s *userSuite) TestPostSerialForgetError(c *check.C) {
	defer daemon.MockDevicestateDeviceManagerUnregister(func(mgr *devicestate.DeviceManager, opts *devicestate.UnregisterOptions) error {
		return errors.New("boom")
	})()

	buf := bytes.NewBufferString(`{"action":"forget"}`)
	req, err := http.NewRequest("POST", "/v2/model/serial", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe, check.DeepEquals, daemon.InternalError(`forgetting serial failed: boom`))
}
