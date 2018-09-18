// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package auth_test

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"

	. "gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type authSuite struct {
	state *state.State

	defURL *url.URL
}

var _ = Suite(&authSuite{})

func (as *authSuite) SetupSuite(c *C) {
	var err error
	as.defURL, err = url.Parse("http://store")
	c.Assert(err, IsNil)
}

func (as *authSuite) SetUpTest(c *C) {
	as.state = state.New(nil)
}

func (s *authSuite) TestMacaroonSerialize(c *C) {
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	c.Check(err, IsNil)

	serialized, err := auth.MacaroonSerialize(m)
	c.Check(err, IsNil)

	deserialized, err := auth.MacaroonDeserialize(serialized)
	c.Check(err, IsNil)
	c.Check(deserialized, DeepEquals, m)
}

func (s *authSuite) TestMacaroonSerializeDeserializeStoreMacaroon(c *C) {
	// sample serialized macaroon using store server setup.
	serialized := `MDAxNmxvY2F0aW9uIGxvY2F0aW9uCjAwMTdpZGVudGlmaWVyIHNvbWUgaWQKMDAwZmNpZCBjYXZlYXQKMDAxOWNpZCAzcmQgcGFydHkgY2F2ZWF0CjAwNTF2aWQgcyvpXSVlMnj9wYw5b-WPCLjTnO_8lVzBrRr8tJfu9tOhPORbsEOFyBwPOM_YiiXJ_qh-Pp8HY0HsUueCUY4dxONLIxPWTdMzCjAwMTJjbCByZW1vdGUuY29tCjAwMmZzaWduYXR1cmUgcm_Gdz75wUCWF9KGXZQEANhwfvBcLNt9xXGfAmxurPMK`

	deserialized, err := auth.MacaroonDeserialize(serialized)
	c.Check(err, IsNil)

	// expected json serialization of the above macaroon
	jsonData := []byte(`{"caveats":[{"cid":"caveat"},{"cid":"3rd party caveat","vid":"cyvpXSVlMnj9wYw5b-WPCLjTnO_8lVzBrRr8tJfu9tOhPORbsEOFyBwPOM_YiiXJ_qh-Pp8HY0HsUueCUY4dxONLIxPWTdMz","cl":"remote.com"}],"location":"location","identifier":"some id","signature":"726fc6773ef9c1409617d2865d940400d8707ef05c2cdb7dc5719f026c6eacf3"}`)

	var expected macaroon.Macaroon
	err = expected.UnmarshalJSON(jsonData)
	c.Check(err, IsNil)
	c.Check(deserialized, DeepEquals, &expected)

	// reserializing the macaroon should give us the same original store serialization
	reserialized, err := auth.MacaroonSerialize(deserialized)
	c.Check(err, IsNil)
	c.Check(reserialized, Equals, serialized)
}

func (s *authSuite) TestMacaroonDeserializeInvalidData(c *C) {
	serialized := "invalid-macaroon-data"

	deserialized, err := auth.MacaroonDeserialize(serialized)
	c.Check(deserialized, IsNil)
	c.Check(err, NotNil)
}

func (as *authSuite) TestNewUser(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	// check snapd macaroon was generated for the local user
	var authStateData auth.AuthState
	as.state.Lock()
	err = as.state.Get("auth", &authStateData)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(authStateData.MacaroonKey, NotNil)
	expectedMacaroon, err := macaroon.New(authStateData.MacaroonKey, "1", "snapd")
	c.Check(err, IsNil)
	expectedSerializedMacaroon, err := auth.MacaroonSerialize(expectedMacaroon)
	c.Check(err, IsNil)

	expected := &auth.UserState{
		ID:              1,
		Username:        "username",
		Email:           "email@test.com",
		Macaroon:        expectedSerializedMacaroon,
		Discharges:      nil,
		StoreMacaroon:   "macaroon",
		StoreDischarges: []string{"discharge"},
	}
	c.Check(user, DeepEquals, expected)
}

func (as *authSuite) TestNewUserSortsDischarges(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "", "email@test.com", "macaroon", []string{"discharge2", "discharge1"})
	c.Assert(err, IsNil)
	as.state.Unlock()

	expected := []string{"discharge1", "discharge2"}
	c.Check(user.StoreDischarges, DeepEquals, expected)

	as.state.Lock()
	userFromState, err := auth.User(as.state, 1)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState.StoreDischarges, DeepEquals, expected)
}

func (as *authSuite) TestNewUserAddsToExistent(c *C) {
	as.state.Lock()
	firstUser, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	// adding a new one
	as.state.Lock()
	user, err := auth.NewUser(as.state, "new_username", "new_email@test.com", "new_macaroon", []string{"new_discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(user.ID, Equals, 2)
	c.Check(user.Username, Equals, "new_username")
	c.Check(user.Email, Equals, "new_email@test.com")

	as.state.Lock()
	userFromState, err := auth.User(as.state, 2)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState.ID, Equals, 2)
	c.Check(userFromState.Username, Equals, "new_username")
	c.Check(userFromState.Email, Equals, "new_email@test.com")

	// first user is still in the state
	as.state.Lock()
	userFromState, err = auth.User(as.state, 1)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, firstUser)
}

func (as *authSuite) TestCheckMacaroonNoAuthData(c *C) {
	as.state.Lock()
	user, err := auth.CheckMacaroon(as.state, "macaroon", []string{"discharge"})
	as.state.Unlock()

	c.Check(err, Equals, auth.ErrInvalidAuth)
	c.Check(user, IsNil)
}

func (as *authSuite) TestCheckMacaroonInvalidAuth(c *C) {
	as.state.Lock()
	user, err := auth.CheckMacaroon(as.state, "other-macaroon", []string{"discharge"})
	as.state.Unlock()

	c.Check(err, Equals, auth.ErrInvalidAuth)
	c.Check(user, IsNil)

	as.state.Lock()
	_, err = auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	user, err = auth.CheckMacaroon(as.state, "other-macaroon", []string{"discharge"})
	as.state.Unlock()

	c.Check(err, Equals, auth.ErrInvalidAuth)
	c.Check(user, IsNil)
}

func (as *authSuite) TestCheckMacaroonValidUser(c *C) {
	as.state.Lock()
	expectedUser, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	user, err := auth.CheckMacaroon(as.state, expectedUser.Macaroon, expectedUser.Discharges)
	as.state.Unlock()

	c.Check(err, IsNil)
	c.Check(user, DeepEquals, expectedUser)
}

func (as *authSuite) TestCheckMacaroonValidUserOldStyle(c *C) {
	// create a fake store-deserializable macaroon
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	c.Check(err, IsNil)
	serializedMacaroon, err := auth.MacaroonSerialize(m)
	c.Check(err, IsNil)

	as.state.Lock()
	expectedUser, err := auth.NewUser(as.state, "username", "email@test.com", serializedMacaroon, []string{"discharge"})
	c.Check(err, IsNil)
	// set user local macaroons with store macaroons
	expectedUser.Macaroon = expectedUser.StoreMacaroon
	expectedUser.Discharges = expectedUser.StoreDischarges
	err = auth.UpdateUser(as.state, expectedUser)
	c.Check(err, IsNil)
	as.state.Unlock()

	as.state.Lock()
	user, err := auth.CheckMacaroon(as.state, expectedUser.Macaroon, expectedUser.Discharges)
	as.state.Unlock()

	c.Check(err, IsNil)
	c.Check(user, DeepEquals, expectedUser)
}

func (as *authSuite) TestCheckMacaroonInvalidAuthMalformedMacaroon(c *C) {
	var authStateData auth.AuthState
	as.state.Lock()
	// create a new user to ensure there is a MacaroonKey setup
	_, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	c.Check(err, IsNil)
	// get AuthState to get signing MacaroonKey
	err = as.state.Get("auth", &authStateData)
	c.Check(err, IsNil)
	as.state.Unlock()

	// setup a macaroon for an invalid user
	invalidMacaroon, err := macaroon.New(authStateData.MacaroonKey, "invalid", "snapd")
	c.Check(err, IsNil)
	serializedInvalidMacaroon, err := auth.MacaroonSerialize(invalidMacaroon)
	c.Check(err, IsNil)

	as.state.Lock()
	user, err := auth.CheckMacaroon(as.state, serializedInvalidMacaroon, nil)
	as.state.Unlock()

	c.Check(err, Equals, auth.ErrInvalidAuth)
	c.Check(user, IsNil)
}

func (as *authSuite) TestUserForNoAuthInState(c *C) {
	as.state.Lock()
	userFromState, err := auth.User(as.state, 42)
	as.state.Unlock()
	c.Check(err, Equals, auth.ErrInvalidUser)
	c.Check(userFromState, IsNil)
}

func (as *authSuite) TestUserForNonExistent(c *C) {
	as.state.Lock()
	_, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	userFromState, err := auth.User(as.state, 42)
	c.Check(err, Equals, auth.ErrInvalidUser)
	c.Check(err, ErrorMatches, "invalid user")
	c.Check(userFromState, IsNil)
}

func (as *authSuite) TestUser(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	userFromState, err := auth.User(as.state, 1)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, user)

	c.Check(user.HasStoreAuth(), Equals, true)
}

func (as *authSuite) TestUserHasStoreAuth(c *C) {
	var user0 *auth.UserState
	// nil user
	c.Check(user0.HasStoreAuth(), Equals, false)

	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(user.HasStoreAuth(), Equals, true)

	// no store auth
	as.state.Lock()
	user, err = auth.NewUser(as.state, "username", "email@test.com", "", nil)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(user.HasStoreAuth(), Equals, false)
}

func (as *authSuite) TestUpdateUser(c *C) {
	as.state.Lock()
	user, _ := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()

	user.Username = "different"
	user.StoreDischarges = []string{"updated-discharge"}

	as.state.Lock()
	err := auth.UpdateUser(as.state, user)
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	userFromState, err := auth.User(as.state, user.ID)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, user)
}

func (as *authSuite) TestUpdateUserInvalid(c *C) {
	as.state.Lock()
	_, _ = auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()

	user := &auth.UserState{
		ID:       102,
		Username: "username",
		Macaroon: "macaroon",
	}

	as.state.Lock()
	err := auth.UpdateUser(as.state, user)
	as.state.Unlock()
	c.Assert(err, Equals, auth.ErrInvalidUser)
}

func (as *authSuite) TestRemove(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	_, err = auth.User(as.state, user.ID)
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	err = auth.RemoveUser(as.state, user.ID)
	as.state.Unlock()
	c.Assert(err, IsNil)

	as.state.Lock()
	_, err = auth.User(as.state, user.ID)
	as.state.Unlock()
	c.Check(err, Equals, auth.ErrInvalidUser)

	as.state.Lock()
	err = auth.RemoveUser(as.state, user.ID)
	as.state.Unlock()
	c.Assert(err, Equals, auth.ErrInvalidUser)
}

func (as *authSuite) TestSetDevice(c *C) {
	as.state.Lock()
	device, err := auth.Device(as.state)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})

	as.state.Lock()
	err = auth.SetDevice(as.state, &auth.DeviceState{Brand: "some-brand"})
	c.Check(err, IsNil)
	device, err = auth.Device(as.state)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{Brand: "some-brand"})
}

func (as *authSuite) TestAuthContextUpdateUserAuth(c *C) {
	as.state.Lock()
	user, _ := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()

	newDischarges := []string{"updated-discharge"}

	authContext := auth.NewAuthContext(as.state, nil)
	user, err := authContext.UpdateUserAuth(user, newDischarges)
	c.Check(err, IsNil)

	as.state.Lock()
	userFromState, err := auth.User(as.state, user.ID)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, user)
	c.Check(userFromState.Discharges, IsNil)
	c.Check(user.StoreDischarges, DeepEquals, newDischarges)
}

func (as *authSuite) TestAuthContextUpdateUserAuthOtherUpdate(c *C) {
	as.state.Lock()
	user, _ := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	otherUpdateUser := *user
	otherUpdateUser.Macaroon = "macaroon2"
	otherUpdateUser.StoreDischarges = []string{"other-discharges"}
	err := auth.UpdateUser(as.state, &otherUpdateUser)
	as.state.Unlock()
	c.Assert(err, IsNil)

	newDischarges := []string{"updated-discharge"}

	authContext := auth.NewAuthContext(as.state, nil)
	// last discharges win
	curUser, err := authContext.UpdateUserAuth(user, newDischarges)
	c.Assert(err, IsNil)

	as.state.Lock()
	userFromState, err := auth.User(as.state, user.ID)
	as.state.Unlock()
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

func (as *authSuite) TestAuthContextUpdateUserAuthInvalid(c *C) {
	as.state.Lock()
	_, _ = auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()

	user := &auth.UserState{
		ID:       102,
		Username: "username",
		Macaroon: "macaroon",
	}

	authContext := auth.NewAuthContext(as.state, nil)
	_, err := authContext.UpdateUserAuth(user, nil)
	c.Assert(err, Equals, auth.ErrInvalidUser)
}

func (as *authSuite) TestAuthContextDeviceForNonExistent(c *C) {
	authContext := auth.NewAuthContext(as.state, nil)

	device, err := authContext.Device()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})
}

func (as *authSuite) TestAuthContextDevice(c *C) {
	device := &auth.DeviceState{Brand: "some-brand"}
	as.state.Lock()
	err := auth.SetDevice(as.state, device)
	as.state.Unlock()
	c.Check(err, IsNil)

	authContext := auth.NewAuthContext(as.state, nil)

	deviceFromState, err := authContext.Device()
	c.Check(err, IsNil)
	c.Check(deviceFromState, DeepEquals, device)
}

func (as *authSuite) TestAuthContextUpdateDeviceAuth(c *C) {
	as.state.Lock()
	device, err := auth.Device(as.state)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})

	sessionMacaroon := "the-device-macaroon"

	authContext := auth.NewAuthContext(as.state, nil)
	device, err = authContext.UpdateDeviceAuth(device, sessionMacaroon)
	c.Check(err, IsNil)

	deviceFromState, err := authContext.Device()
	c.Check(err, IsNil)
	c.Check(deviceFromState, DeepEquals, device)
	c.Check(deviceFromState.SessionMacaroon, DeepEquals, sessionMacaroon)
}

func (as *authSuite) TestAuthContextUpdateDeviceAuthOtherUpdate(c *C) {
	as.state.Lock()
	device, _ := auth.Device(as.state)
	otherUpdateDevice := *device
	otherUpdateDevice.SessionMacaroon = "othe-session-macaroon"
	otherUpdateDevice.KeyID = "KEYID"
	err := auth.SetDevice(as.state, &otherUpdateDevice)
	as.state.Unlock()
	c.Check(err, IsNil)

	sessionMacaroon := "the-device-macaroon"

	authContext := auth.NewAuthContext(as.state, nil)
	curDevice, err := authContext.UpdateDeviceAuth(device, sessionMacaroon)
	c.Assert(err, IsNil)

	as.state.Lock()
	deviceFromState, err := auth.Device(as.state)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(deviceFromState, DeepEquals, curDevice)
	c.Check(curDevice, DeepEquals, &auth.DeviceState{
		KeyID:           "KEYID",
		SessionMacaroon: sessionMacaroon,
	})
}

func (as *authSuite) TestAuthContextStoreParamsFallback(c *C) {
	authContext := auth.NewAuthContext(as.state, nil)

	storeID, err := authContext.StoreID("store-id")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "store-id")

	proxyStoreID, proxyStoreURL, err := authContext.ProxyStoreParams(as.defURL)
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "")
	c.Check(proxyStoreURL, Equals, as.defURL)
}

func (as *authSuite) TestAuthContextStoreIDFromEnv(c *C) {
	authContext := auth.NewAuthContext(as.state, nil)

	os.Setenv("UBUNTU_STORE_ID", "env-store-id")
	defer os.Unsetenv("UBUNTU_STORE_ID")
	storeID, err := authContext.StoreID("")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "env-store-id")
}

func (as *authSuite) TestAuthContextDeviceSessionRequestParamsNilDeviceAssertions(c *C) {
	authContext := auth.NewAuthContext(as.state, nil)

	_, err := authContext.DeviceSessionRequestParams("NONCE")
	c.Check(err, Equals, auth.ErrNoSerial)
}

func (as *authSuite) TestAuthContextCloudInfo(c *C) {
	authContext := auth.NewAuthContext(as.state, nil)

	cloud, err := authContext.CloudInfo()
	c.Assert(err, IsNil)
	c.Check(cloud, IsNil)

	cloudInfo := &auth.CloudInfo{
		Name:             "aws",
		Region:           "us-east-1",
		AvailabilityZone: "us-east-1a",
	}
	as.state.Lock()
	defer as.state.Unlock()
	tr := config.NewTransaction(as.state)
	tr.Set("core", "cloud", cloudInfo)
	tr.Commit()

	as.state.Unlock()
	cloud, err = authContext.CloudInfo()
	as.state.Lock()
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

func (da *testDeviceAssertions) DeviceSessionRequestParams(nonce string) (*auth.DeviceSessionRequestParams, error) {
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

	return &auth.DeviceSessionRequestParams{
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

func (as *authSuite) TestAuthContextMissingDeviceAssertions(c *C) {
	// no assertions in state
	authContext := auth.NewAuthContext(as.state, &testDeviceAssertions{nothing: true})

	_, err := authContext.DeviceSessionRequestParams("NONCE")
	c.Check(err, Equals, auth.ErrNoSerial)

	storeID, err := authContext.StoreID("fallback")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "fallback")

	proxyStoreID, proxyStoreURL, err := authContext.ProxyStoreParams(as.defURL)
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "")
	c.Check(proxyStoreURL, Equals, as.defURL)
}

func (as *authSuite) TestAuthContextWithDeviceAssertions(c *C) {
	// having assertions in state
	authContext := auth.NewAuthContext(as.state, &testDeviceAssertions{})

	params, err := authContext.DeviceSessionRequestParams("NONCE-1")
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
	storeID, err := authContext.StoreID("store-id")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "my-brand-store-id")

	// proxy store
	fooURL, err := url.Parse("http://foo.internal")
	c.Assert(err, IsNil)

	proxyStoreID, proxyStoreURL, err := authContext.ProxyStoreParams(as.defURL)
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "foo")
	c.Check(proxyStoreURL, DeepEquals, fooURL)
}

func (as *authSuite) TestAuthContextWithDeviceAssertionsGenericClassicModel(c *C) {
	model, err := asserts.Decode([]byte(exModel))
	c.Assert(err, IsNil)
	// (ab)use the example as the generic classic model
	r := sysdb.MockGenericClassicModel(model.(*asserts.Model))
	defer r()
	// having assertions in state
	authContext := auth.NewAuthContext(as.state, &testDeviceAssertions{})

	// for the generic classic model we continue to consider the env var
	os.Setenv("UBUNTU_STORE_ID", "env-store-id")
	defer os.Unsetenv("UBUNTU_STORE_ID")
	storeID, err := authContext.StoreID("store-id")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "env-store-id")
}

func (as *authSuite) TestAuthContextWithDeviceAssertionsGenericClassicModelNoEnvVar(c *C) {
	model, err := asserts.Decode([]byte(exModel))
	c.Assert(err, IsNil)
	// (ab)use the example as the generic classic model
	r := sysdb.MockGenericClassicModel(model.(*asserts.Model))
	defer r()
	// having assertions in state
	authContext := auth.NewAuthContext(as.state, &testDeviceAssertions{})

	// for the generic classic model we continue to consider the env var
	// but when the env var is unset we don't do anything wrong.
	os.Unsetenv("UBUNTU_STORE_ID")
	storeID, err := authContext.StoreID("store-id")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "store-id")
}

func (as *authSuite) TestUsers(c *C) {
	as.state.Lock()
	user1, err1 := auth.NewUser(as.state, "user1", "email1@test.com", "macaroon", []string{"discharge"})
	user2, err2 := auth.NewUser(as.state, "user2", "email2@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err1, IsNil)
	c.Check(err2, IsNil)

	as.state.Lock()
	users, err := auth.Users(as.state)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(users, DeepEquals, []*auth.UserState{user1, user2})
}

func (as *authSuite) TestEnsureContexts(c *C) {
	ctx1 := auth.EnsureContextTODO()
	ctx2 := auth.EnsureContextTODO()

	c.Check(ctx1, Not(Equals), ctx2)

	c.Check(auth.IsEnsureContext(ctx1), Equals, true)
	c.Check(auth.IsEnsureContext(ctx2), Equals, true)

	c.Check(auth.IsEnsureContext(context.TODO()), Equals, false)
}
