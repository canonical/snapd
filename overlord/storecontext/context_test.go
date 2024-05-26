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
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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
	s.defURL = mylog.Check2(url.Parse("http://store"))

}

func (s *storeCtxSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

func (s *storeCtxSuite) TestUpdateUserAuth(c *C) {
	s.state.Lock()
	user, _ := auth.NewUser(s.state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	s.state.Unlock()

	newDischarges := []string{"updated-discharge"}

	storeCtx := storecontext.New(s.state, &testBackend{nothing: true})
	user := mylog.Check2(storeCtx.UpdateUserAuth(user, newDischarges))
	c.Check(err, IsNil)

	s.state.Lock()
	userFromState := mylog.Check2(auth.User(s.state, user.ID))
	s.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, user)
	c.Check(userFromState.Discharges, IsNil)
	c.Check(user.StoreDischarges, DeepEquals, newDischarges)
}

func (s *storeCtxSuite) TestUpdateUserAuthOtherUpdate(c *C) {
	s.state.Lock()
	user, _ := auth.NewUser(s.state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	otherUpdateUser := *user
	otherUpdateUser.Macaroon = "macaroon2"
	otherUpdateUser.StoreDischarges = []string{"other-discharges"}
	mylog.Check(auth.UpdateUser(s.state, &otherUpdateUser))
	s.state.Unlock()


	newDischarges := []string{"updated-discharge"}

	storeCtx := storecontext.New(s.state, &testBackend{nothing: true})
	// last discharges win
	curUser := mylog.Check2(storeCtx.UpdateUserAuth(user, newDischarges))


	s.state.Lock()
	userFromState := mylog.Check2(auth.User(s.state, user.ID))
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
	_, _ = auth.NewUser(s.state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	s.state.Unlock()

	user := &auth.UserState{
		ID:       102,
		Username: "username",
		Macaroon: "macaroon",
	}

	storeCtx := storecontext.New(s.state, &testBackend{nothing: true})
	_ := mylog.Check2(storeCtx.UpdateUserAuth(user, nil))
	c.Assert(err, Equals, auth.ErrInvalidUser)
}

func (s *storeCtxSuite) TestDeviceForNonExistent(c *C) {
	storeCtx := storecontext.New(s.state, &testBackend{nothing: true})

	device := mylog.Check2(storeCtx.Device())
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})
}

func (s *storeCtxSuite) TestDevice(c *C) {
	device := &auth.DeviceState{Brand: "some-brand"}
	storeCtx := storecontext.New(s.state, &testBackend{device: device})

	deviceFromState := mylog.Check2(storeCtx.Device())
	c.Check(err, IsNil)
	c.Check(deviceFromState, DeepEquals, device)
}

func (s *storeCtxSuite) TestUpdateDeviceAuth(c *C) {
	device := &auth.DeviceState{}
	storeCtx := storecontext.New(s.state, &testBackend{device: device})

	sessionMacaroon := "the-device-macaroon"
	device := mylog.Check2(storeCtx.UpdateDeviceAuth(device, sessionMacaroon))
	c.Check(err, IsNil)

	deviceFromState := mylog.Check2(storeCtx.Device())
	c.Check(err, IsNil)
	c.Check(deviceFromState, DeepEquals, device)
	c.Check(deviceFromState.SessionMacaroon, DeepEquals, sessionMacaroon)
}

func (s *storeCtxSuite) TestUpdateDeviceAuthOtherUpdate(c *C) {
	device := &auth.DeviceState{}
	otherUpdateDevice := *device
	otherUpdateDevice.SessionMacaroon = "other-session-macaroon"
	otherUpdateDevice.KeyID = "KEYID"

	b := &testBackend{device: &otherUpdateDevice}
	storeCtx := storecontext.New(s.state, b)

	// the global store refreshing sessions is now serialized
	// and is a no-op in this case, but we do need not to overwrite
	// the result of a remodeling (though unlikely as we will mostly avoid
	// other store operations during it)
	sessionMacaroon := "the-device-macaroon"
	curDevice := mylog.Check2(storeCtx.UpdateDeviceAuth(device, sessionMacaroon))


	c.Check(b.device, DeepEquals, curDevice)
	c.Check(curDevice, DeepEquals, &auth.DeviceState{
		KeyID:           "KEYID",
		SessionMacaroon: "other-session-macaroon",
	})
}

func (s *storeCtxSuite) TestStoreParamsFallback(c *C) {
	storeCtx := storecontext.New(s.state, &testBackend{nothing: true})

	storeID := mylog.Check2(storeCtx.StoreID("store-id"))

	c.Check(storeID, Equals, "store-id")

	proxyStoreID, proxyStoreURL := mylog.Check3(storeCtx.ProxyStoreParams(s.defURL))

	c.Check(proxyStoreID, Equals, "")
	c.Check(proxyStoreURL, Equals, s.defURL)
}

func (s *storeCtxSuite) TestStoreIDFromEnv(c *C) {
	storeCtx := storecontext.New(s.state, &testBackend{nothing: true})

	os.Setenv("UBUNTU_STORE_ID", "env-store-id")
	defer os.Unsetenv("UBUNTU_STORE_ID")
	storeID := mylog.Check2(storeCtx.StoreID(""))

	c.Check(storeID, Equals, "env-store-id")
}

func (s *storeCtxSuite) TestCloudInfo(c *C) {
	storeCtx := storecontext.New(s.state, &testBackend{nothing: true})

	cloud := mylog.Check2(storeCtx.CloudInfo())

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
	cloud = mylog.Check2(storeCtx.CloudInfo())
	s.state.Lock()

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

type testBackend struct {
	nothing      bool
	noSerial     bool
	storeOffline bool
	device       *auth.DeviceState
}

func (b *testBackend) Device() (*auth.DeviceState, error) {
	freshDevice := auth.DeviceState{}
	if b.device != nil {
		freshDevice = *b.device
	}
	return &freshDevice, nil
}

func (b *testBackend) SetDevice(d *auth.DeviceState) error {
	*b.device = *d
	return nil
}

func (b *testBackend) Model() (*asserts.Model, error) {
	if b.nothing {
		return nil, state.ErrNoState
	}
	a := mylog.Check2(asserts.Decode([]byte(exModel)))

	return a.(*asserts.Model), nil
}

func (b *testBackend) Serial() (*asserts.Serial, error) {
	if b.nothing || b.noSerial {
		return nil, state.ErrNoState
	}
	a := mylog.Check2(asserts.Decode([]byte(exSerial)))

	return a.(*asserts.Serial), nil
}

func (b *testBackend) SignDeviceSessionRequest(serial *asserts.Serial, nonce string) (*asserts.DeviceSessionRequest, error) {
	if b.nothing {
		return nil, state.ErrNoState
	}

	ex := strings.Replace(exDeviceSessionRequest, "@NONCE@", nonce, 1)
	ex = strings.Replace(ex, "@TS@", time.Now().Format(time.RFC3339), 1)
	aReq := mylog.Check2(asserts.Decode([]byte(ex)))

	return aReq.(*asserts.DeviceSessionRequest), nil
}

func (b *testBackend) ProxyStore() (*asserts.Store, error) {
	if b.nothing {
		return nil, state.ErrNoState
	}
	a := mylog.Check2(asserts.Decode([]byte(exStore)))

	return a.(*asserts.Store), nil
}

func (b *testBackend) StoreOffline() (bool, error) {
	if b.nothing {
		return false, state.ErrNoState
	}

	return b.storeOffline, nil
}

func (s *storeCtxSuite) TestMissingDeviceAssertions(c *C) {
	// no assertions in state
	storeCtx := storecontext.New(s.state, &testBackend{nothing: true})

	_ := mylog.Check2(storeCtx.DeviceSessionRequestParams("NONCE"))
	c.Check(err, Equals, store.ErrNoSerial)

	storeID := mylog.Check2(storeCtx.StoreID("fallback"))

	c.Check(storeID, Equals, "fallback")

	proxyStoreID, proxyStoreURL := mylog.Check3(storeCtx.ProxyStoreParams(s.defURL))

	c.Check(proxyStoreID, Equals, "")
	c.Check(proxyStoreURL, Equals, s.defURL)
}

func (s *storeCtxSuite) TestWithDeviceAssertions(c *C) {
	// having assertions in state
	storeCtx := storecontext.New(s.state, &testBackend{})

	params := mylog.Check2(storeCtx.DeviceSessionRequestParams("NONCE-1"))


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
	storeID := mylog.Check2(storeCtx.StoreID("store-id"))

	c.Check(storeID, Equals, "my-brand-store-id")

	// proxy store
	fooURL := mylog.Check2(url.Parse("http://foo.internal"))


	proxyStoreID, proxyStoreURL := mylog.Check3(storeCtx.ProxyStoreParams(s.defURL))

	c.Check(proxyStoreID, Equals, "foo")
	c.Check(proxyStoreURL, DeepEquals, fooURL)
}

func (s *storeCtxSuite) TestWithDeviceAssertionsGenericClassicModel(c *C) {
	model := mylog.Check2(asserts.Decode([]byte(exModel)))

	// (ab)use the example as the generic classic model
	r := sysdb.MockGenericClassicModel(model.(*asserts.Model))
	defer r()
	// having assertions in state
	storeCtx := storecontext.New(s.state, &testBackend{})

	// for the generic classic model we continue to consider the env var
	os.Setenv("UBUNTU_STORE_ID", "env-store-id")
	defer os.Unsetenv("UBUNTU_STORE_ID")
	storeID := mylog.Check2(storeCtx.StoreID("store-id"))

	c.Check(storeID, Equals, "env-store-id")
}

func (s *storeCtxSuite) TestWithDeviceAssertionsGenericClassicModelNoEnvVar(c *C) {
	model := mylog.Check2(asserts.Decode([]byte(exModel)))

	// (ab)use the example as the generic classic model
	r := sysdb.MockGenericClassicModel(model.(*asserts.Model))
	defer r()
	// having assertions in state
	storeCtx := storecontext.New(s.state, &testBackend{})

	// for the generic classic model we continue to consider the env var
	// but when the env var is unset we don't do anything wrong.
	os.Unsetenv("UBUNTU_STORE_ID")
	storeID := mylog.Check2(storeCtx.StoreID("store-id"))

	c.Check(storeID, Equals, "store-id")
}

type testFailingDeviceSessionRequestSigner struct{}

func (srqs testFailingDeviceSessionRequestSigner) SignDeviceSessionRequest(serial *asserts.Serial, nonce string) (*asserts.DeviceSessionRequest, error) {
	return nil, errors.New("boom")
}

func (s *storeCtxSuite) TestComposable(c *C) {
	b := &testBackend{}
	bNoSerial := &testBackend{noSerial: true}

	storeCtx := storecontext.NewComposed(s.state, b, bNoSerial, b)

	params := mylog.Check2(storeCtx.DeviceSessionRequestParams("NONCE-1"))


	req := params.EncodedRequest()
	c.Check(strings.Contains(req, "nonce: NONCE-1\n"), Equals, true)
	c.Check(strings.Contains(req, "serial: 9999\n"), Equals, true)

	storeCtx = storecontext.NewComposed(s.state, bNoSerial, b, b)
	params = mylog.Check2(storeCtx.DeviceSessionRequestParams("NONCE-1"))
	c.Assert(err, Equals, store.ErrNoSerial)

	srqs := testFailingDeviceSessionRequestSigner{}
	storeCtx = storecontext.NewComposed(s.state, b, srqs, b)
	params = mylog.Check2(storeCtx.DeviceSessionRequestParams("NONCE-1"))
	c.Assert(err, ErrorMatches, "boom")
}

func (s *storeCtxSuite) TestStoreOffline(c *C) {
	b := &testBackend{
		storeOffline: true,
	}

	storeCtx := storecontext.NewComposed(s.state, b, b, b)

	offline := mylog.Check2(storeCtx.StoreOffline())
	c.Check(err, IsNil)
	c.Check(offline, Equals, true)
}
