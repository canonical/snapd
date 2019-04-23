// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

package storecontext_test

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/store"
)

func Test(t *testing.T) { TestingT(t) }

type storeCtxSuite struct {
	state *state.State

	defURL *url.URL
}

var _ = Suite(&storeCtxSuite{})

func (s *storeCtxSuite) SetupSuite(c *C) {
	var err error
	s.defURL, err = url.Parse("http://store")
	c.Assert(err, IsNil)
}

func (s *storeCtxSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

func (s *storeCtxSuite) TestUpdateUserAuth(c *C) {
	s.state.Lock()
	user, _ := auth.NewUser(s.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	s.state.Unlock()

	newDischarges := []string{"updated-discharge"}

	storeCtx := storecontext.New(s.state, nil)
	user, err := storeCtx.UpdateUserAuth(user, newDischarges)
	c.Check(err, IsNil)

	s.state.Lock()
	userFromState, err := auth.User(s.state, user.ID)
	s.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, user)
	c.Check(userFromState.Discharges, IsNil)
	c.Check(user.StoreDischarges, DeepEquals, newDischarges)
}

func (s *storeCtxSuite) TestUpdateUserAuthOtherUpdate(c *C) {
	s.state.Lock()
	user, _ := auth.NewUser(s.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	otherUpdateUser := *user
	otherUpdateUser.Macaroon = "macaroon2"
	otherUpdateUser.StoreDischarges = []string{"other-discharges"}
	err := auth.UpdateUser(s.state, &otherUpdateUser)
	s.state.Unlock()
	c.Assert(err, IsNil)

	newDischarges := []string{"updated-discharge"}

	storeCtx := storecontext.New(s.state, nil)
	// last discharges win
	curUser, err := storeCtx.UpdateUserAuth(user, newDischarges)
	c.Assert(err, IsNil)

	s.state.Lock()
	userFromState, err := auth.User(s.state, user.ID)
	s.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, curUser)
	c.Check(curUser, DeepEquals, &auth.UserState{
		ID:              user.ID,
		Username:        "username",
		Email:           "email@test.com",
		Macaroon:        "macaroon2",
		Discharges:      nil,
		StoreMacaroon:   "macaroon",
		StoreDischarges: newDischarges,
	})
}

func (s *storeCtxSuite) TestUpdateUserAuthInvalid(c *C) {
	s.state.Lock()
	_, _ = auth.NewUser(s.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	s.state.Unlock()

	user := &auth.UserState{
		ID:       102,
		Username: "username",
		Macaroon: "macaroon",
	}

	storeCtx := storecontext.New(s.state, nil)
	_, err := storeCtx.UpdateUserAuth(user, nil)
	c.Assert(err, Equals, auth.ErrInvalidUser)
}

func (s *storeCtxSuite) TestDeviceForNonExistent(c *C) {
	storeCtx := storecontext.New(s.state, nil)

	device, err := storeCtx.Device()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})
}

func (s *storeCtxSuite) TestDevice(c *C) {
	device := &auth.DeviceState{Brand: "some-brand"}
	s.state.Lock()
	err := auth.SetDevice(s.state, device)
	s.state.Unlock()
	c.Check(err, IsNil)

	storeCtx := storecontext.New(s.state, nil)

	deviceFromState, err := storeCtx.Device()
	c.Check(err, IsNil)
	c.Check(deviceFromState, DeepEquals, device)
}

func (s *storeCtxSuite) TestUpdateDeviceAuth(c *C) {
	s.state.Lock()
	device, err := auth.Device(s.state)
	s.state.Unlock()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})

	sessionMacaroon := "the-device-macaroon"

	storeCtx := storecontext.New(s.state, nil)
	device, err = storeCtx.UpdateDeviceAuth(device, sessionMacaroon)
	c.Check(err, IsNil)

	deviceFromState, err := storeCtx.Device()
	c.Check(err, IsNil)
	c.Check(deviceFromState, DeepEquals, device)
	c.Check(deviceFromState.SessionMacaroon, DeepEquals, sessionMacaroon)
}

func (s *storeCtxSuite) TestUpdateDeviceAuthOtherUpdate(c *C) {
	s.state.Lock()
	device, _ := auth.Device(s.state)
	otherUpdateDevice := *device
	otherUpdateDevice.SessionMacaroon = "othe-session-macaroon"
	otherUpdateDevice.KeyID = "KEYID"
	err := auth.SetDevice(s.state, &otherUpdateDevice)
	s.state.Unlock()
	c.Check(err, IsNil)

	sessionMacaroon := "the-device-macaroon"

	storeCtx := storecontext.New(s.state, nil)
	curDevice, err := storeCtx.UpdateDeviceAuth(device, sessionMacaroon)
	c.Assert(err, IsNil)

	s.state.Lock()
	deviceFromState, err := auth.Device(s.state)
	s.state.Unlock()
	c.Check(err, IsNil)
	c.Check(deviceFromState, DeepEquals, curDevice)
	c.Check(curDevice, DeepEquals, &auth.DeviceState{
		KeyID:           "KEYID",
		SessionMacaroon: sessionMacaroon,
	})
}

func (s *storeCtxSuite) TestStoreParamsFallback(c *C) {
	storeCtx := storecontext.New(s.state, nil)

	storeID, err := storeCtx.StoreID("store-id")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "store-id")

	proxyStoreID, proxyStoreURL, err := storeCtx.ProxyStoreParams(s.defURL)
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "")
	c.Check(proxyStoreURL, Equals, s.defURL)
}

func (s *storeCtxSuite) TestStoreIDFromEnv(c *C) {
	storeCtx := storecontext.New(s.state, nil)

	os.Setenv("UBUNTU_STORE_ID", "env-store-id")
	defer os.Unsetenv("UBUNTU_STORE_ID")
	storeID, err := storeCtx.StoreID("")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "env-store-id")
}

func (s *storeCtxSuite) TestDeviceSessionRequestParamsNilDeviceAssertions(c *C) {
	storeCtx := storecontext.New(s.state, nil)

	_, err := storeCtx.DeviceSessionRequestParams("NONCE")
	c.Check(err, Equals, store.ErrNoSerial)
}

func (s *storeCtxSuite) TestCloudInfo(c *C) {
	storeCtx := storecontext.New(s.state, nil)

	cloud, err := storeCtx.CloudInfo()
	c.Assert(err, IsNil)
	c.Check(cloud, IsNil)

	cloudInfo := &auth.CloudInfo{
		Name:             "aws",
		Region:           "us-east-1",
		AvailabilityZone: "us-east-1a",
	}
	s.state.Lock()
	defer s.state.Unlock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "cloud", cloudInfo)
	tr.Commit()

	s.state.Unlock()
	cloud, err = storeCtx.CloudInfo()
	s.state.Lock()
	c.Assert(err, IsNil)
	c.Check(cloud, DeepEquals, cloudInfo)
}

const (
	exModel = `type: model
authority-id: my-brand
series: 16
brand-id: my-brand
model: baz-3000
architecture: armhf
gadget: gadget
kernel: kernel
store: my-brand-store-id
timestamp: 2016-08-20T13:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=`

	exSerial = `type: serial
authority-id: my-brand
brand-id: my-brand
model: baz-3000
serial: 9999
device-key:
    AcbBTQRWhcGAARAAtJGIguK7FhSyRxL/6jvdy0zAgGCjC1xVNFzeF76p5G8BXNEEHZUHK+z8Gr2J
    inVrpvhJhllf5Ob2dIMH2YQbC9jE1kjbzvuauQGDqk6tNQm0i3KDeHCSPgVN+PFXPwKIiLrh66Po
    AC7OfR1rFUgCqu0jch0H6Nue0ynvEPiY4dPeXq7mCdpDr5QIAM41L+3hg0OdzvO8HMIGZQpdF6jP
    7fkkVMROYvHUOJ8kknpKE7FiaNNpH7jK1qNxOYhLeiioX0LYrdmTvdTWHrSKZc82ZmlDjpKc4hUx
    VtTXMAysw7CzIdREPom/vJklnKLvZt+Wk5AEF5V5YKnuT3pY+fjVMZ56GtTEeO/Er/oLk/n2xUK5
    fD5DAyW/9z0ygzwTbY5IuWXyDfYneL4nXwWOEgg37Z4+8mTH+ftTz2dl1x1KIlIR2xo0kxf9t8K+
    jlr13vwF1+QReMCSUycUsZ2Eep5XhjI+LG7G1bMSGqodZTIOXLkIy6+3iJ8Z/feIHlJ0ELBDyFbl
    Yy04Sf9LI148vJMsYenonkoWejWdMi8iCUTeaZydHJEUBU/RbNFLjCWa6NIUe9bfZgLiOOZkps54
    +/AL078ri/tGjo/5UGvezSmwrEoWJyqrJt2M69N2oVDLJcHeo2bUYPtFC2Kfb2je58JrJ+llifdg
    rAsxbnHXiXyVimUAEQEAAQ==
device-key-sha3-384: EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu
timestamp: 2016-08-24T21:55:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=`

	exDeviceSessionRequest = `type: device-session-request
brand-id: my-brand
model: baz-3000
serial: 9999
nonce: @NONCE@
timestamp: @TS@
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=`

	exStore = `type: store
authority-id: canonical
store: foo
operator-id: foo-operator
url: http://foo.internal
timestamp: 2017-11-01T10:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=`
)

type testDeviceAssertions struct {
	nothing bool
}

func (da *testDeviceAssertions) Model() (*asserts.Model, error) {
	if da.nothing {
		return nil, state.ErrNoState
	}
	a, err := asserts.Decode([]byte(exModel))
	if err != nil {
		return nil, err
	}
	return a.(*asserts.Model), nil
}

func (da *testDeviceAssertions) Serial() (*asserts.Serial, error) {
	if da.nothing {
		return nil, state.ErrNoState
	}
	a, err := asserts.Decode([]byte(exSerial))
	if err != nil {
		return nil, err
	}
	return a.(*asserts.Serial), nil
}

func (da *testDeviceAssertions) DeviceSessionRequestParams(nonce string) (*storecontext.DeviceSessionRequestParams, error) {
	if da.nothing {
		return nil, state.ErrNoState
	}
	ex := strings.Replace(exDeviceSessionRequest, "@NONCE@", nonce, 1)
	ex = strings.Replace(ex, "@TS@", time.Now().Format(time.RFC3339), 1)
	aReq, err := asserts.Decode([]byte(ex))
	if err != nil {
		return nil, err
	}

	aSer, err := asserts.Decode([]byte(exSerial))
	if err != nil {
		return nil, err
	}

	aMod, err := asserts.Decode([]byte(exModel))
	if err != nil {
		return nil, err
	}

	return &storecontext.DeviceSessionRequestParams{
		Request: aReq.(*asserts.DeviceSessionRequest),
		Serial:  aSer.(*asserts.Serial),
		Model:   aMod.(*asserts.Model),
	}, nil
}

func (da *testDeviceAssertions) ProxyStore() (*asserts.Store, error) {
	if da.nothing {
		return nil, state.ErrNoState
	}
	a, err := asserts.Decode([]byte(exStore))
	if err != nil {
		return nil, err
	}
	return a.(*asserts.Store), nil
}

func (s *storeCtxSuite) TestMissingDeviceAssertions(c *C) {
	// no assertions in state
	storeCtx := storecontext.New(s.state, &testDeviceAssertions{nothing: true})

	_, err := storeCtx.DeviceSessionRequestParams("NONCE")
	c.Check(err, Equals, store.ErrNoSerial)

	storeID, err := storeCtx.StoreID("fallback")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "fallback")

	proxyStoreID, proxyStoreURL, err := storeCtx.ProxyStoreParams(s.defURL)
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "")
	c.Check(proxyStoreURL, Equals, s.defURL)
}

func (s *storeCtxSuite) TestWithDeviceAssertions(c *C) {
	// having assertions in state
	storeCtx := storecontext.New(s.state, &testDeviceAssertions{})

	params, err := storeCtx.DeviceSessionRequestParams("NONCE-1")
	c.Assert(err, IsNil)

	req := params.EncodedRequest()
	serial := params.EncodedSerial()
	model := params.EncodedModel()

	c.Check(strings.Contains(req, "nonce: NONCE-1\n"), Equals, true)
	c.Check(strings.Contains(req, "serial: 9999\n"), Equals, true)

	c.Check(strings.Contains(serial, "model: baz-3000\n"), Equals, true)
	c.Check(strings.Contains(serial, "serial: 9999\n"), Equals, true)
	c.Check(strings.Contains(model, "model: baz-3000\n"), Equals, true)
	c.Check(strings.Contains(model, "serial:\n"), Equals, false)

	// going to be ignored
	os.Setenv("UBUNTU_STORE_ID", "env-store-id")
	defer os.Unsetenv("UBUNTU_STORE_ID")
	storeID, err := storeCtx.StoreID("store-id")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "my-brand-store-id")

	// proxy store
	fooURL, err := url.Parse("http://foo.internal")
	c.Assert(err, IsNil)

	proxyStoreID, proxyStoreURL, err := storeCtx.ProxyStoreParams(s.defURL)
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "foo")
	c.Check(proxyStoreURL, DeepEquals, fooURL)
}

func (s *storeCtxSuite) TestWithDeviceAssertionsGenericClassicModel(c *C) {
	model, err := asserts.Decode([]byte(exModel))
	c.Assert(err, IsNil)
	// (ab)use the example as the generic classic model
	r := sysdb.MockGenericClassicModel(model.(*asserts.Model))
	defer r()
	// having assertions in state
	storeCtx := storecontext.New(s.state, &testDeviceAssertions{})

	// for the generic classic model we continue to consider the env var
	os.Setenv("UBUNTU_STORE_ID", "env-store-id")
	defer os.Unsetenv("UBUNTU_STORE_ID")
	storeID, err := storeCtx.StoreID("store-id")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "env-store-id")
}

func (s *storeCtxSuite) TestWithDeviceAssertionsGenericClassicModelNoEnvVar(c *C) {
	model, err := asserts.Decode([]byte(exModel))
	c.Assert(err, IsNil)
	// (ab)use the example as the generic classic model
	r := sysdb.MockGenericClassicModel(model.(*asserts.Model))
	defer r()
	// having assertions in state
	storeCtx := storecontext.New(s.state, &testDeviceAssertions{})

	// for the generic classic model we continue to consider the env var
	// but when the env var is unset we don't do anything wrong.
	os.Unsetenv("UBUNTU_STORE_ID")
	storeID, err := storeCtx.StoreID("store-id")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "store-id")
}
