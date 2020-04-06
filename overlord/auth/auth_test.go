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

package auth_test

import (
	"context"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
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

func (as *authSuite) TestUserByUsername(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	userFromState, err := auth.UserByUsername(as.state, "username")
	as.state.Unlock()
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, user)

	as.state.Lock()
	_, err = auth.UserByUsername(as.state, "otherusername")
	as.state.Unlock()
	c.Check(err, Equals, auth.ErrInvalidUser)
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
	u, err := auth.RemoveUser(as.state, user.ID)
	as.state.Unlock()
	c.Assert(err, IsNil)
	c.Check(u, DeepEquals, &auth.UserState{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",
	})

	as.state.Lock()
	_, err = auth.User(as.state, user.ID)
	as.state.Unlock()
	c.Check(err, Equals, auth.ErrInvalidUser)

	as.state.Lock()
	_, err = auth.RemoveUser(as.state, user.ID)
	as.state.Unlock()
	c.Assert(err, Equals, auth.ErrInvalidUser)
}

func (as *authSuite) TestRemoveByUsername(c *C) {
	as.state.Lock()
	user, err := auth.NewUser(as.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	_, err = auth.User(as.state, user.ID)
	as.state.Unlock()
	c.Check(err, IsNil)

	as.state.Lock()
	u, err := auth.RemoveUserByUsername(as.state, user.Username)
	as.state.Unlock()
	c.Assert(err, IsNil)
	c.Check(u, DeepEquals, &auth.UserState{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",
	})

	as.state.Lock()
	_, err = auth.User(as.state, user.ID)
	as.state.Unlock()
	c.Check(err, Equals, auth.ErrInvalidUser)

	as.state.Lock()
	_, err = auth.RemoveUserByUsername(as.state, user.Username)
	as.state.Unlock()
	c.Assert(err, Equals, auth.ErrInvalidUser)
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
