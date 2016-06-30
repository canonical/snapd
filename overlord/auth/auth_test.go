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
	"fmt"
	"net/http"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type authSuite struct {
	state *state.State
}

var _ = Suite(&authSuite{})

func (as *authSuite) SetUpTest(c *C) {
	as.state = state.New(nil)
}

func (as *authSuite) TestNewUser(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
	as.state.Unlock()

	expected := &auth.UserState{
		ID:              1,
		Username:        "username",
		Macaroon:        "macaroon",
		Discharges:      []string{"discharge"},
		StoreMacaroon:   "macaroon",
		StoreDischarges: []string{"discharge"},
	}
	c.Check(err, IsNil)
	c.Check(user, DeepEquals, expected)

	as.state.Lock()
	userFromState, err := auth.User(as.state, 1)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, expected)
}

func (as *authSuite) TestNewUserSortsDischarges(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge2", "discharge1"})
	as.state.Unlock()

	expected := &auth.UserState{
		ID:              1,
		Username:        "username",
		Macaroon:        "macaroon",
		Discharges:      []string{"discharge1", "discharge2"},
		StoreMacaroon:   "macaroon",
		StoreDischarges: []string{"discharge1", "discharge2"},
	}
	c.Check(err, IsNil)
	c.Check(user, DeepEquals, expected)

	as.state.Lock()
	userFromState, err := auth.User(as.state, 1)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, expected)
}

func (as *authSuite) TestNewUserAddsToExistent(c *C) {
	as.state.Lock()
	firstUser, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	// adding a new one
	as.state.Lock()
	user, err := auth.NewUser(as.state, "new_username", "new_macaroon", []string{"new_discharge"})
	as.state.Unlock()
	expected := &auth.UserState{
		ID:              2,
		Username:        "new_username",
		Macaroon:        "new_macaroon",
		Discharges:      []string{"new_discharge"},
		StoreMacaroon:   "new_macaroon",
		StoreDischarges: []string{"new_discharge"},
	}
	c.Check(err, IsNil)
	c.Check(user, DeepEquals, expected)

	as.state.Lock()
	userFromState, err := auth.User(as.state, 2)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, expected)

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
	_, err = auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
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
	expectedUser, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	user, err := auth.CheckMacaroon(as.state, "macaroon", []string{"discharge"})
	as.state.Unlock()

	c.Check(err, IsNil)
	c.Check(user, DeepEquals, expectedUser)
}

func (as *authSuite) TestUserForNoAuthInState(c *C) {
	as.state.Lock()
	userFromState, err := auth.User(as.state, 42)
	as.state.Unlock()
	c.Check(err, NotNil)
	c.Check(userFromState, IsNil)
}

func (as *authSuite) TestUserForNonExistent(c *C) {
	as.state.Lock()
	_, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	userFromState, err := auth.User(as.state, 42)
	c.Check(err, ErrorMatches, "invalid user")
	c.Check(userFromState, IsNil)
}

func (as *authSuite) TestUser(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	userFromState, err := auth.User(as.state, 1)
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, user)
}

func (as *authSuite) TestRemove(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
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
	c.Check(err, ErrorMatches, "invalid user")

	as.state.Lock()
	err = auth.RemoveUser(as.state, user.ID)
	as.state.Unlock()
	c.Assert(err, ErrorMatches, "invalid user")
}

func (as *authSuite) makeTestMacaroon() (*macaroon.Macaroon, error) {
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	if err != nil {
		return nil, err
	}
	err = m.AddFirstPartyCaveat("first-party-caveat")
	if err != nil {
		return nil, err
	}
	err = m.AddThirdPartyCaveat([]byte("shared-key"), "third-party-caveat", store.UbuntuoneLocation)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (as *authSuite) makeTestDischarge() (*macaroon.Macaroon, error) {
	m, err := macaroon.New([]byte("shared-key"), "third-party-caveat", store.UbuntuoneLocation)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (as *authSuite) TestMacaroonSerialize(c *C) {
	m, err := as.makeTestMacaroon()
	c.Check(err, IsNil)

	serialized, err := auth.MacaroonSerialize(m)
	c.Check(err, IsNil)

	deserialized, err := auth.MacaroonDeserialize(serialized)
	c.Check(err, IsNil)
	c.Check(deserialized, DeepEquals, m)
}

func (as *authSuite) TestMacaroonDeserializeStoreMacaroon(c *C) {
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
}

func (as *authSuite) TestMacaroonDeserializeInvalidData(c *C) {
	serialized := "invalid-macaroon-data"

	deserialized, err := auth.MacaroonDeserialize(serialized)
	c.Check(deserialized, IsNil)
	c.Check(err, NotNil)
}

func (as *authSuite) TestLoginCaveatIDReturnCaveatID(c *C) {
	m, err := as.makeTestMacaroon()
	c.Check(err, IsNil)

	caveat, err := auth.LoginCaveatID(m)
	c.Check(err, IsNil)
	c.Check(caveat, Equals, "third-party-caveat")
}

func (as *authSuite) TestLoginCaveatIDMacaroonMissingCaveat(c *C) {
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	c.Check(err, IsNil)

	caveat, err := auth.LoginCaveatID(m)
	c.Check(err, NotNil)
	c.Check(caveat, Equals, "")
}

func (as *authSuite) TestGetAuthenticatorFromUser(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	authenticator := user.Authenticator()
	c.Check(authenticator.Macaroon, Equals, user.Macaroon)
	c.Check(authenticator.Discharges, DeepEquals, user.Discharges)
}

func (as *authSuite) TestAuthenticatorSetHeaders(c *C) {
	root, err := as.makeTestMacaroon()
	c.Check(err, IsNil)
	discharge, err := as.makeTestDischarge()
	c.Check(err, IsNil)

	serializedMacaroon, err := auth.MacaroonSerialize(root)
	c.Check(err, IsNil)
	serializedDischarge, err := auth.MacaroonSerialize(discharge)
	c.Check(err, IsNil)

	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", serializedMacaroon, []string{serializedDischarge})
	as.state.Unlock()
	c.Check(err, IsNil)

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	authenticator := user.Authenticator()
	authenticator.Authenticate(req)

	// discharge macaroon should be bound to the root macaroon
	discharge.Bind(root.Signature())
	serializedPreparedDischarge, err := auth.MacaroonSerialize(discharge)
	c.Check(err, IsNil)

	authorization := req.Header.Get("Authorization")
	expected := fmt.Sprintf(`Macaroon root="%s", discharge="%s"`, serializedMacaroon, serializedPreparedDischarge)
	c.Check(authorization, Equals, expected)
}
