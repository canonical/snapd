// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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
	"fmt"
	"net/http"
	"os/user"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

var _ = check.Suite(&userSuite{})

type userSuite struct {
	apiBaseSuite

	loginUserStoreMacaroon string
	loginUserDischarge     string

	mockUserHome      string
	trivialUserLookup func(username string) (*user.User, error)
}

func (s *userSuite) LoginUser(username, password, otp string) (string, string, error) {
	s.pokeStateLock()

	return s.loginUserStoreMacaroon, s.loginUserDischarge, s.err
}

func (s *userSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.AddCleanup(release.MockOnClassic(false))

	s.daemonWithStore(c, s)

	s.expectRootAccess()

	s.mockUserHome = c.MkDir()
	s.trivialUserLookup = mkUserLookup(s.mockUserHome)
	// s.AddCleanup(devicestatetest.MockUserLookup(s.trivialUserLookup))

	s.AddCleanup(daemon.MockHasUserAdmin(true))

	// make sure we don't call these by accident)
	s.AddCleanup(daemon.MockDeviceStateCreateUser(func(st *state.State, mgr *devicestate.DeviceManager, sudoer bool, createKnown bool, email string) (createdUsers []devicestate.UserResponse, internal_err bool, err error) {
		c.Fatalf("unexpected create user %q call", email)
		return nil, false, fmt.Errorf("unexpected create user %q call", email)
	}))

	s.AddCleanup(daemon.MockDeviceStateRemoveUser(func(st *state.State, username string) (*auth.UserState, bool, error) {
		c.Fatalf("unexpected remove user %q call", username)
		return nil, false, fmt.Errorf("unexpected remove user %q call", username)
	}))

	s.loginUserStoreMacaroon = ""
	s.loginUserDischarge = ""
}

func mkUserLookup(userHomeDir string) func(string) (*user.User, error) {
	return func(username string) (*user.User, error) {
		cur, err := user.Current()
		cur.Username = username
		cur.HomeDir = userHomeDir
		return cur, err
	}
}

func (s *userSuite) expectLoginAccess() {
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.login"})
}

func (s *userSuite) TestLoginUser(c *check.C) {
	state := s.d.Overlord().State()

	s.expectLoginAccess()

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)

	expected := daemon.UserResponseData{
		ID:    1,
		Email: "email@.com",

		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}

	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	c.Check(user.ID, check.Equals, 1)
	c.Check(user.Username, check.Equals, "")
	c.Check(user.Email, check.Equals, "email@.com")
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
	// snapd macaroon was setup too
	snapdMacaroon, err := auth.MacaroonDeserialize(user.Macaroon)
	c.Check(err, check.IsNil)
	c.Check(snapdMacaroon.Id(), check.Equals, "1")
	c.Check(snapdMacaroon.Location(), check.Equals, "snapd")
}

func (s *userSuite) TestLoginUserWithUsername(c *check.C) {
	state := s.d.Overlord().State()

	s.expectLoginAccess()

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "username", "email": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)

	expected := daemon.UserResponseData{
		ID:         1,
		Username:   "username",
		Email:      "email@.com",
		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	c.Check(user.ID, check.Equals, 1)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, "email@.com")
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
	// snapd macaroon was setup too
	snapdMacaroon, err := auth.MacaroonDeserialize(user.Macaroon)
	c.Check(err, check.IsNil)
	c.Check(snapdMacaroon.Id(), check.Equals, "1")
	c.Check(snapdMacaroon.Location(), check.Equals, "snapd")
}

func (s *userSuite) TestLoginUserNoEmailWithExistentLocalUser(c *check.C) {
	state := s.d.Overlord().State()

	s.expectLoginAccess()

	// setup local-only user
	state.Lock()
	localUser, err := auth.NewUser(state, "username", "email@test.com", "", nil)
	state.Unlock()
	c.Assert(err, check.IsNil)

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "username", "email": "", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, localUser)

	expected := daemon.UserResponseData{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	state.Lock()
	user, err := auth.User(state, localUser.ID)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, localUser.Email)
	c.Check(user.Macaroon, check.Equals, localUser.Macaroon)
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
}

func (s *userSuite) TestLoginUserWithExistentLocalUser(c *check.C) {
	state := s.d.Overlord().State()

	s.expectLoginAccess()

	// setup local-only user
	state.Lock()
	localUser, err := auth.NewUser(state, "username", "email@test.com", "", nil)
	state.Unlock()
	c.Assert(err, check.IsNil)

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "username", "email": "email@test.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, localUser)

	expected := daemon.UserResponseData{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	state.Lock()
	user, err := auth.User(state, localUser.ID)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, localUser.Email)
	c.Check(user.Macaroon, check.Equals, localUser.Macaroon)
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
}

func (s *userSuite) TestLoginUserNewEmailWithExistentLocalUser(c *check.C) {
	state := s.d.Overlord().State()

	s.expectLoginAccess()

	// setup local-only user
	state.Lock()
	localUser, err := auth.NewUser(state, "username", "email@test.com", "", nil)
	state.Unlock()
	c.Assert(err, check.IsNil)

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	// same local user, but using a new SSO account
	buf := bytes.NewBufferString(`{"username": "username", "email": "new.email@test.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, localUser)

	expected := daemon.UserResponseData{
		ID:       1,
		Username: "username",
		Email:    "new.email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	state.Lock()
	user, err := auth.User(state, localUser.ID)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, expected.Email)
	c.Check(user.Macaroon, check.Equals, localUser.Macaroon)
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
}

func (s *userSuite) TestLogoutUser(c *check.C) {
	state := s.d.Overlord().State()

	s.expectLoginAccess()

	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/logout", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, user)
	c.Check(rsp.Status, check.Equals, 200)

	state.Lock()
	_, err = auth.User(state, user.ID)
	state.Unlock()
	c.Check(err, check.Equals, auth.ErrInvalidUser)
}

func (s *userSuite) TestLoginUserBadRequest(c *check.C) {
	s.expectLoginAccess()

	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Not(check.Equals), "")
}

func (s *userSuite) TestLoginUserNotEmailish(c *check.C) {
	s.expectLoginAccess()

	buf := bytes.NewBufferString(`{"username": "notemail", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, testutil.Contains, "please use a valid email address")
}

func (s *userSuite) TestLoginUserDeveloperAPIError(c *check.C) {
	s.expectLoginAccess()

	s.err = fmt.Errorf("error-from-login-user")
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 401)
	c.Check(rspe.Message, testutil.Contains, "error-from-login-user")
}

func (s *userSuite) TestLoginUserTwoFactorRequiredError(c *check.C) {
	s.expectLoginAccess()

	s.err = store.ErrAuthenticationNeeds2fa
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 401)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindTwoFactorRequired)
}

func (s *userSuite) TestLoginUserTwoFactorFailedError(c *check.C) {
	s.expectLoginAccess()

	s.err = store.Err2faFailed
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 401)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindTwoFactorFailed)
}

func (s *userSuite) TestLoginUserInvalidCredentialsError(c *check.C) {
	s.expectLoginAccess()

	s.err = store.ErrInvalidCredentials
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 401)
	c.Check(rspe.Message, check.Equals, "invalid credentials")
}

func (s *userSuite) TestLoginUserInvalidAuthDataError(c *check.C) {
	s.expectLoginAccess()

	s.err = store.InvalidAuthDataError{"foo": {"bar"}}
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindInvalidAuthData)
	c.Check(rspe.Value, check.DeepEquals, s.err)
}

func (s *userSuite) TestLoginUserPasswordPolicyError(c *check.C) {
	s.expectLoginAccess()

	s.err = store.PasswordPolicyError{"foo": {"bar"}}
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 401)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindPasswordPolicy)
	c.Check(rspe.Value, check.DeepEquals, s.err)
}

func (s *userSuite) TestPostCreateUser(c *check.C) {
	s.testCreateUser(c, true)
}

func (s *userSuite) TestPostUserCreate(c *check.C) {
	s.testCreateUser(c, false)
}

func (s *userSuite) testCreateUser(c *check.C, oldWay bool) {
	expectedUsername := "karl"
	expectedEmail := "popper@lse.ac.uk"

	defer daemon.MockDeviceStateCreateUser(func(st *state.State, mgr *devicestate.DeviceManager, sudoer bool, createKnown bool, email string) (createdUsers []devicestate.UserResponse, internal_err bool, err error) {
		c.Check(email, check.Equals, expectedEmail)
		c.Check(sudoer, check.Equals, false)
		expected := []devicestate.UserResponse{
			{
				Username: expectedUsername,
				SSHKeys: []string{
					`ssh1 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
					`ssh2 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
				},
			},
		}
		return expected, false, nil
	})()

	var req *http.Request
	var expected interface{}
	expectedItem := daemon.UserResponseData{
		Username: expectedUsername,
		SSHKeys: []string{
			`ssh1 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
			`ssh2 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
		},
	}

	if oldWay {
		var err error
		buf := bytes.NewBufferString(fmt.Sprintf(`{"email": "%s"}`, expectedEmail))
		req, err = http.NewRequest("POST", "/v2/create-user", buf)
		c.Assert(err, check.IsNil)
		expected = []daemon.UserResponseData{expectedItem}
	} else {
		var err error
		buf := bytes.NewBufferString(fmt.Sprintf(`{"action":"create","email": "%s"}`, expectedEmail))
		req, err = http.NewRequest("POST", "/v2/users", buf)
		c.Assert(err, check.IsNil)
		expected = []daemon.UserResponseData{expectedItem}
	}

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *userSuite) TestPostUserCreateErrBadRequest(c *check.C) {
	s.testCreateUserErr(c, false)
}

func (s *userSuite) TestPostUserCreateErrInternal(c *check.C) {
	s.testCreateUserErr(c, true)
}

func (s *userSuite) testCreateUserErr(c *check.C, internal_err bool) {
	called := 0
	defer daemon.MockDeviceStateCreateUser(func(st *state.State, mgr *devicestate.DeviceManager, sudoer bool, createKnown bool, email string) ([]devicestate.UserResponse, bool, error) {
		called++
		if internal_err {
			return nil, internal_err, fmt.Errorf("wat-internal")
		} else {
			return nil, internal_err, fmt.Errorf("wat-badrequest")
		}
	})()

	buf := bytes.NewBufferString(`{"email": "foo@bar.com","known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(called, check.Equals, 1)
	if internal_err {
		c.Check(rspe.Status, check.Equals, 500)
		c.Check(rspe.Message, check.Equals, "wat-internal")
	} else {
		c.Check(rspe.Status, check.Equals, 400)
		c.Check(rspe.Message, check.Equals, "wat-badrequest")
	}
}

func (s *userSuite) TestNoUserAdminCreateUser(c *check.C) { s.testNoUserAdmin(c, "/v2/create-user") }

func (s *userSuite) TestNoUserAdminPostUser(c *check.C) { s.testNoUserAdmin(c, "/v2/users") }

func (s *userSuite) testNoUserAdmin(c *check.C, endpoint string) {
	defer daemon.MockHasUserAdmin(false)()

	buf := bytes.NewBufferString("{}")
	req, err := http.NewRequest("POST", endpoint, buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)

	const noUserAdmin = "system user administration via snapd is not allowed on this system"
	switch endpoint {
	case "/v2/users":
		c.Check(rspe, check.DeepEquals, daemon.MethodNotAllowed(noUserAdmin))
	case "/v2/create-user":
		c.Check(rspe, check.DeepEquals, daemon.Forbidden(noUserAdmin))
	default:
		c.Fatalf("unknown endpoint %q", endpoint)
	}
}

func (s *userSuite) TestPostUserBadBody(c *check.C) {
	buf := bytes.NewBufferString(`42`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Matches, "cannot decode user action data from request body: .*")
}

func (s *userSuite) TestPostUserBadAfterBody(c *check.C) {
	buf := bytes.NewBufferString(`{}42`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe, check.DeepEquals, daemon.BadRequest("spurious content after user action"))
}

func (s *userSuite) TestPostUserNoAction(c *check.C) {
	buf := bytes.NewBufferString("{}")
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe, check.DeepEquals, daemon.BadRequest("missing user action"))
}

func (s *userSuite) TestPostUserBadAction(c *check.C) {
	buf := bytes.NewBufferString(`{"action":"patatas"}`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe, check.DeepEquals, daemon.BadRequest(`unsupported user action "patatas"`))
}

func (s *userSuite) TestPostUserActionRemoveDelUserErrBadRequest(c *check.C) {
	s.testpostUserActionRemoveDelUserErr(c, false)
}

func (s *userSuite) TestPostUserActionRemoveDelUserErrInternal(c *check.C) {
	s.testpostUserActionRemoveDelUserErr(c, true)
}

func (s *userSuite) testpostUserActionRemoveDelUserErr(c *check.C, internal_err bool) {
	st := s.d.Overlord().State()
	st.Lock()
	_, err := auth.NewUser(st, "some-user", "email@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Check(err, check.IsNil)

	called := 0
	defer daemon.MockDeviceStateRemoveUser(func(st *state.State, username string) (*auth.UserState, bool, error) {
		called++
		if internal_err {
			return nil, internal_err, fmt.Errorf("wat-internal")
		} else {
			return nil, internal_err, fmt.Errorf("wat-badrequest")
		}
	})()

	buf := bytes.NewBufferString(`{"action":"remove","username":"some-user"}`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(called, check.Equals, 1)
	if internal_err {
		c.Check(rspe.Status, check.Equals, 500)
		c.Check(rspe.Message, check.Equals, "wat-internal")
	} else {
		c.Check(rspe.Status, check.Equals, 400)
		c.Check(rspe.Message, check.Equals, "wat-badrequest")
	}
}

func (s *userSuite) TestPostUserActionRemove(c *check.C) {
	expectedId := 10
	expectedUsername := "some-user"
	expedtedEmail := "email@test.com"

	called := 0
	defer daemon.MockDeviceStateRemoveUser(func(st *state.State, username string) (*auth.UserState, bool, error) {
		called++
		removedUser := &auth.UserState{ID: expectedId, Username: expectedUsername, Email: expedtedEmail}

		return removedUser, false, nil
	})()

	buf := bytes.NewBufferString(`{"action":"remove","username":"some-user"}`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 200)
	expected := []daemon.UserResponseData{
		{ID: expectedId, Username: expectedUsername, Email: expedtedEmail},
	}
	c.Check(rsp.Result, check.DeepEquals, map[string]interface{}{
		"removed": expected,
	})
	c.Check(called, check.Equals, 1)
}

func (s *userSuite) setupSigner(accountID string, signerPrivKey asserts.PrivateKey) *assertstest.SigningDB {
	st := s.d.Overlord().State()

	signerSigning := s.Brands.Register(accountID, signerPrivKey, map[string]interface{}{
		"account-id":   accountID,
		"verification": "verified",
	})
	acctNKey := s.Brands.AccountsAndKeys(accountID)

	assertstest.AddMany(s.StoreSigning, acctNKey...)
	assertstatetest.AddMany(st, acctNKey...)

	return signerSigning
}

var (
	partnerPrivKey, _ = assertstest.GenerateKey(752)
	unknownPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *userSuite) makeSystemUsers(c *check.C, systemUsers []map[string]interface{}) {
	st := s.d.Overlord().State()
	st.Lock()
	defer st.Unlock()

	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""))

	s.setupSigner("my-brand", brandPrivKey)
	s.setupSigner("partner", partnerPrivKey)
	s.setupSigner("unknown", unknownPrivKey)

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":          "amd64",
		"gadget":                "pc",
		"kernel":                "pc-kernel",
		"required-snaps":        []interface{}{"required-snap1"},
		"system-user-authority": []interface{}{"my-brand", "partner"},
	})
	// now add model related stuff to the system
	assertstatetest.AddMany(st, model)
	// and a serial
	deviceKey, _ := assertstest.GenerateKey(752)
	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, check.IsNil)
	serial, err := s.Brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-model",
		"serial":              "serialserial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	assertstatetest.AddMany(st, serial)

	for _, suMap := range systemUsers {
		su, err := s.Brands.Signing(suMap["authority-id"].(string)).Sign(asserts.SystemUserType, suMap, nil, "")
		c.Assert(err, check.IsNil)
		su = su.(*asserts.SystemUser)
		// now add system-user assertion to the system
		assertstatetest.AddMany(st, su)
	}
	// create fake device
	err = devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserial",
	})
	c.Assert(err, check.IsNil)
}

var goodUser = map[string]interface{}{
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "foo@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model", "other-model"},
	"name":         "Boring Guy",
	"username":     "guy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

func (s *userSuite) TestPostCreateUserFromAssertionAllKnownClassicErrors(c *check.C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Matches, `cannot create user: device is a classic system`)
}

func (s *userSuite) TestPostCreateUserFromAssertionAllKnownButOwnedErrors(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	st := s.d.Overlord().State()
	st.Lock()
	_, err := auth.NewUser(st, "username", "email@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Check(err, check.IsNil)

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Matches, `cannot create user: device already managed`)
}

func (s *userSuite) TestPostCreateUserAutomaticManagedDoesNotActOrError(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	st := s.d.Overlord().State()
	st.Lock()
	_, err := auth.NewUser(st, "username", "email@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Check(err, check.IsNil)

	// do it!
	buf := bytes.NewBufferString(`{"automatic":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	// expecting an empty reply
	expected := []daemon.UserResponseData{}
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *userSuite) TestPostCreateUserAutomaticDisabled(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	// disable automatic user creation
	st := s.d.Overlord().State()
	st.Lock()
	tr := config.NewTransaction(st)
	err := tr.Set("core", "users.create.automatic", false)
	tr.Commit()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// if there is attempt to cresate user, test should panic

	// do it!
	buf := bytes.NewBufferString(`{"automatic": true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	// empty result
	expected := []daemon.UserResponseData{}
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	// ensure no user was added to the state
	st.Lock()
	users, err := auth.Users(st)
	c.Assert(err, check.IsNil)
	st.Unlock()
	c.Check(users, check.HasLen, 0)
}

func (s *userSuite) TestUsersEmpty(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/users", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	expected := []daemon.UserResponseData{}
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *userSuite) TestUsersHasUser(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	u, err := auth.NewUser(st, "someuser", "mymail@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/users", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	expected := []daemon.UserResponseData{
		{ID: u.ID, Username: u.Username, Email: u.Email},
	}
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}
