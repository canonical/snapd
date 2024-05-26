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

	"github.com/ddkwork/golibrary/mylog"
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

	s.AddCleanup(daemon.MockHasUserAdmin(true))

	// make sure we don't call these by accident)
	s.AddCleanup(daemon.MockDeviceStateCreateUser(func(st *state.State, sudoer bool, email string, expiration time.Time) (createdUsers *devicestate.CreatedUser, internalErr error) {
		c.Fatalf("unexpected create user %q call", email)
		return nil, &devicestate.UserError{Err: fmt.Errorf("unexpected create user %q call", email)}
	}))

	s.AddCleanup(daemon.MockDeviceStateCreateKnownUsers(func(st *state.State, sudoer bool, email string) (createdUsers []*devicestate.CreatedUser, internalErr error) {
		c.Fatalf("unexpected create user %q call", email)
		return nil, &devicestate.UserError{Err: fmt.Errorf("unexpected create user %q call", email)}
	}))

	s.AddCleanup(daemon.MockDeviceStateRemoveUser(func(st *state.State, username string, opts *devicestate.RemoveUserOptions) (*auth.UserState, error) {
		c.Fatalf("unexpected remove user %q call", username)
		return nil, &devicestate.UserError{Err: fmt.Errorf("unexpected remove user %q call", username)}
	}))

	s.loginUserStoreMacaroon = ""
	s.loginUserDischarge = ""
}

func mkUserLookup(userHomeDir string) func(string) (*user.User, error) {
	return func(username string) (*user.User, error) {
		cur := mylog.Check2(user.Current())
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
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	state.Lock()
	user := mylog.Check2(auth.User(state, 1))
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
	snapdMacaroon := mylog.Check2(auth.MacaroonDeserialize(user.Macaroon))
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
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	state.Lock()
	user := mylog.Check2(auth.User(state, 1))
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
	snapdMacaroon := mylog.Check2(auth.MacaroonDeserialize(user.Macaroon))
	c.Check(err, check.IsNil)
	c.Check(snapdMacaroon.Id(), check.Equals, "1")
	c.Check(snapdMacaroon.Location(), check.Equals, "snapd")
}

func (s *userSuite) TestLoginUserNoEmailWithExistentLocalUser(c *check.C) {
	state := s.d.Overlord().State()

	s.expectLoginAccess()

	// setup local-only user
	state.Lock()
	localUser := mylog.Check2(auth.NewUser(state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "",
		Discharges: nil,
	}))
	state.Unlock()
	c.Assert(err, check.IsNil)

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "username", "email": "", "password": "password"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
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
	user := mylog.Check2(auth.User(state, localUser.ID))
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
	localUser := mylog.Check2(auth.NewUser(state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "",
		Discharges: nil,
	}))
	state.Unlock()
	c.Assert(err, check.IsNil)

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "username", "email": "email@test.com", "password": "password"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
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
	user := mylog.Check2(auth.User(state, localUser.ID))
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
	localUser := mylog.Check2(auth.NewUser(state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "",
		Discharges: nil,
	}))
	state.Unlock()
	c.Assert(err, check.IsNil)

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	// same local user, but using a new SSO account
	buf := bytes.NewBufferString(`{"username": "username", "email": "new.email@test.com", "password": "password"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
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
	user := mylog.Check2(auth.User(state, localUser.ID))
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
	user := mylog.Check2(auth.NewUser(state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}))
	state.Unlock()
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/logout", nil))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, user)
	c.Check(rsp.Status, check.Equals, 200)

	state.Lock()
	_ = mylog.Check2(auth.User(state, user.ID))
	state.Unlock()
	c.Check(err, check.Equals, auth.ErrInvalidUser)
}

func (s *userSuite) TestLoginUserBadRequest(c *check.C) {
	s.expectLoginAccess()

	buf := bytes.NewBufferString(`hello`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Not(check.Equals), "")
}

func (s *userSuite) TestLoginUserNotEmailish(c *check.C) {
	s.expectLoginAccess()

	buf := bytes.NewBufferString(`{"username": "notemail", "password": "password"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, testutil.Contains, "please use a valid email address")
}

func (s *userSuite) TestLoginUserDeveloperAPIError(c *check.C) {
	s.expectLoginAccess()

	s.err = fmt.Errorf("error-from-login-user")
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 401)
	c.Check(rspe.Message, testutil.Contains, "error-from-login-user")
}

func (s *userSuite) TestLoginUserTwoFactorRequiredError(c *check.C) {
	s.expectLoginAccess()

	s.err = store.ErrAuthenticationNeeds2fa
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 401)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindTwoFactorRequired)
}

func (s *userSuite) TestLoginUserTwoFactorFailedError(c *check.C) {
	s.expectLoginAccess()

	s.err = store.Err2faFailed
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 401)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindTwoFactorFailed)
}

func (s *userSuite) TestLoginUserInvalidCredentialsError(c *check.C) {
	s.expectLoginAccess()

	s.err = store.ErrInvalidCredentials
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 401)
	c.Check(rspe.Message, check.Equals, "invalid credentials")
}

func (s *userSuite) TestLoginUserInvalidAuthDataError(c *check.C) {
	s.expectLoginAccess()

	s.err = store.InvalidAuthDataError{"foo": {"bar"}}
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
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
	req := mylog.Check2(http.NewRequest("POST", "/v2/login", buf))
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

	var deviceStateCreateUserCalled bool
	defer daemon.MockDeviceStateCreateUser(func(st *state.State, sudoer bool, email string, expiration time.Time) (*devicestate.CreatedUser, error) {
		c.Check(email, check.Equals, expectedEmail)
		c.Check(sudoer, check.Equals, false)
		c.Check(expiration, check.Equals, time.Time{})
		expected := &devicestate.CreatedUser{
			Username: expectedUsername,
			SSHKeys: []string{
				`ssh1 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
				`ssh2 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
			},
		}
		deviceStateCreateUserCalled = true
		return expected, nil
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

		buf := bytes.NewBufferString(fmt.Sprintf(`{"email": "%s"}`, expectedEmail))
		req = mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
		c.Assert(err, check.IsNil)
		expected = &expectedItem
	} else {

		buf := bytes.NewBufferString(fmt.Sprintf(`{"action":"create","email": "%s"}`, expectedEmail))
		req = mylog.Check2(http.NewRequest("POST", "/v2/users", buf))
		c.Assert(err, check.IsNil)
		expected = []daemon.UserResponseData{expectedItem}
	}

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
	c.Check(deviceStateCreateUserCalled, check.Equals, true)
}

func (s *userSuite) TestPostUserCreateErrBadRequest(c *check.C) {
	s.testCreateUserErr(c, false)
}

func (s *userSuite) TestPostUserCreateErrInternal(c *check.C) {
	s.testCreateUserErr(c, true)
}

func (s *userSuite) testCreateUserErr(c *check.C, internalErr bool) {
	called := 0
	defer daemon.MockDeviceStateCreateKnownUsers(func(st *state.State, sudoer bool, email string) ([]*devicestate.CreatedUser, error) {
		called++
		if internalErr {
			return nil, fmt.Errorf("internal error: wat-internal")
		} else {
			return nil, &devicestate.UserError{Err: fmt.Errorf("wat-badrequest")}
		}
	})()

	buf := bytes.NewBufferString(`{"email": "foo@bar.com","known":true}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(called, check.Equals, 1)
	if internalErr {
		c.Check(rspe.Status, check.Equals, 500)
		c.Check(rspe.Message, check.Equals, "internal error: wat-internal")
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
	req := mylog.Check2(http.NewRequest("POST", endpoint, buf))
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
	req := mylog.Check2(http.NewRequest("POST", "/v2/users", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Matches, "cannot decode user action data from request body: .*")
}

func (s *userSuite) TestPostUserBadAfterBody(c *check.C) {
	buf := bytes.NewBufferString(`{}42`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/users", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe, check.DeepEquals, daemon.BadRequest("spurious content after user action"))
}

func (s *userSuite) TestPostUserNoAction(c *check.C) {
	buf := bytes.NewBufferString("{}")
	req := mylog.Check2(http.NewRequest("POST", "/v2/users", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe, check.DeepEquals, daemon.BadRequest("missing user action"))
}

func (s *userSuite) TestPostUserBadAction(c *check.C) {
	buf := bytes.NewBufferString(`{"action":"patatas"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/users", buf))
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

func (s *userSuite) testpostUserActionRemoveDelUserErr(c *check.C, internalErr bool) {
	st := s.d.Overlord().State()
	st.Lock()
	_ := mylog.Check2(auth.NewUser(st, auth.NewUserParams{
		Username:   "some-user",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}))
	st.Unlock()
	c.Check(err, check.IsNil)

	called := 0
	defer daemon.MockDeviceStateRemoveUser(func(st *state.State, username string, opts *devicestate.RemoveUserOptions) (*auth.UserState, error) {
		called++
		if internalErr {
			return nil, fmt.Errorf("internal error: wat-internal")
		} else {
			return nil, &devicestate.UserError{Err: fmt.Errorf("wat-badrequest")}
		}
	})()

	buf := bytes.NewBufferString(`{"action":"remove","username":"some-user"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/users", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(called, check.Equals, 1)
	if internalErr {
		c.Check(rspe.Status, check.Equals, 500)
		c.Check(rspe.Message, check.Equals, "internal error: wat-internal")
	} else {
		c.Check(rspe.Status, check.Equals, 400)
		c.Check(rspe.Message, check.Equals, "wat-badrequest")
	}
}

func (s *userSuite) TestPostUserActionRemove(c *check.C) {
	expectedID := 10
	expectedUsername := "some-user"
	expectedEmail := "email@test.com"

	called := 0
	defer daemon.MockDeviceStateRemoveUser(func(st *state.State, username string, opts *devicestate.RemoveUserOptions) (*auth.UserState, error) {
		called++
		removedUser := &auth.UserState{ID: expectedID, Username: expectedUsername, Email: expectedEmail}

		return removedUser, nil
	})()

	buf := bytes.NewBufferString(`{"action":"remove","username":"some-user"}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/users", buf))
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 200)
	expected := []daemon.UserResponseData{
		{ID: expectedID, Username: expectedUsername, Email: expectedEmail},
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
	encDevKey := mylog.Check2(asserts.EncodePublicKey(deviceKey.PublicKey()))
	c.Assert(err, check.IsNil)
	serial := mylog.Check2(s.Brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-model",
		"serial":              "serialserial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, ""))
	c.Assert(err, check.IsNil)
	assertstatetest.AddMany(st, serial)

	for _, suMap := range systemUsers {
		su := mylog.Check2(s.Brands.Signing(suMap["authority-id"].(string)).Sign(asserts.SystemUserType, suMap, nil, ""))
		c.Assert(err, check.IsNil)
		su = su.(*asserts.SystemUser)
		// now add system-user assertion to the system
		assertstatetest.AddMany(st, su)
	}
	mylog.
		// create fake device
		Check(devicestatetest.SetDevice(st, &auth.DeviceState{
			Brand:  "my-brand",
			Model:  "my-model",
			Serial: "serialserial",
		}))
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

var partnerUser = map[string]interface{}{
	"authority-id": "partner",
	"brand-id":     "my-brand",
	"email":        "p@partner.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"name":         "Partner Guy",
	"username":     "partnerguy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var serialUser = map[string]interface{}{
	"format":       "1",
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "serial@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"serials":      []interface{}{"serialserial"},
	"name":         "Serial Guy",
	"username":     "goodserialguy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var badUser = map[string]interface{}{
	// bad user (not valid for this model)
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "foobar@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"non-of-the-models-i-have"},
	"name":         "Random Gal",
	"username":     "gal",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var badUserNoMatchingSerial = map[string]interface{}{
	"format":       "1",
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "noserial@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"serials":      []interface{}{"different-serialserial"},
	"name":         "No Serial Guy",
	"username":     "noserial",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var unknownUser = map[string]interface{}{
	"authority-id": "unknown",
	"brand-id":     "my-brand",
	"email":        "x@partner.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"name":         "XGuy",
	"username":     "xguy",
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
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Matches, `cannot create user: device is a classic system`)
}

func (s *userSuite) TestPostCreateUserFromAssertionAllKnownButOwnedErrors(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	st := s.d.Overlord().State()
	st.Lock()
	_ := mylog.Check2(auth.NewUser(st, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}))
	st.Unlock()
	c.Check(err, check.IsNil)

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Matches, `cannot create user: device already managed`)
}

func (s *userSuite) TestPostCreateUserAutomaticManagedDoesNotActOrError(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	st := s.d.Overlord().State()
	st.Lock()
	_ := mylog.Check2(auth.NewUser(st, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}))
	st.Unlock()
	c.Check(err, check.IsNil)

	// do it!
	buf := bytes.NewBufferString(`{"automatic":true}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
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
	mylog.Check(tr.Set("core", "users.create.automatic", false))
	tr.Commit()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// if there is attempt to create user, test should panic

	// do it!
	buf := bytes.NewBufferString(`{"automatic": true}`)
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	// empty result
	expected := []daemon.UserResponseData{}
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	// ensure no user was added to the state
	st.Lock()
	users := mylog.Check2(auth.Users(st))
	c.Assert(err, check.IsNil)
	st.Unlock()
	c.Check(users, check.HasLen, 0)
}

func (s *userSuite) TestPostCreateUserExpirationHappy(c *check.C) {
	expectedUsername := "karl"
	expectedEmail := "popper@lse.ac.uk"
	// strip away subsecond values which are lost during json marshal/unmarshalling
	expectedTime := time.Now().Add(time.Hour * 24).Round(time.Second)

	var deviceStateCreateUserCalls int
	defer daemon.MockDeviceStateCreateUser(func(st *state.State, sudoer bool, email string, expiration time.Time) (*devicestate.CreatedUser, error) {
		c.Check(email, check.Equals, expectedEmail)
		c.Check(sudoer, check.Equals, false)
		c.Check(expiration.Equal(expectedTime), check.Equals, true)
		expected := &devicestate.CreatedUser{
			Username: expectedUsername,
			SSHKeys: []string{
				`ssh1 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
				`ssh2 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
			},
		}
		deviceStateCreateUserCalls++
		return expected, nil
	})()

	buf := bytes.NewBufferString(fmt.Sprintf(`{"email":"%s","expiration":"%s"}`,
		expectedEmail, expectedTime.Format(time.RFC3339)))
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Result, check.DeepEquals, &daemon.UserResponseData{
		Username: expectedUsername,
		SSHKeys: []string{
			`ssh1 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
			`ssh2 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
		},
	})
	c.Check(deviceStateCreateUserCalls, check.Equals, 1)
}

func (s *userSuite) TestPostCreateUserExpirationDateSetInPast(c *check.C) {
	buf := bytes.NewBufferString(fmt.Sprintf(`{"expiration":"%s"}`,
		time.Now().Add(-(time.Hour * 24)).Format(time.RFC3339)))
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Matches, `cannot create user: expiration date must be set in the future`)
}

func (s *userSuite) TestPostCreateUserExpirationKnownNotAllowed(c *check.C) {
	buf := bytes.NewBufferString(fmt.Sprintf(`{"known": true, "expiration":"%s"}`,
		time.Now().Add(time.Hour*24).Format(time.RFC3339)))
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Matches, `cannot create user: expiration date cannot be provided for known users`)
}

func (s *userSuite) TestPostCreateUserExpirationAutomaticNotAllowed(c *check.C) {
	// Automatic implies known, which means we should see identical result to
	// the known unit test
	buf := bytes.NewBufferString(fmt.Sprintf(`{"automatic": true, "expiration":"%s"}`,
		time.Now().Add(time.Hour*24).Format(time.RFC3339)))
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Matches, `cannot create user: expiration date cannot be provided for known users`)
}

func (s *userSuite) TestUsersEmpty(c *check.C) {
	req := mylog.Check2(http.NewRequest("GET", "/v2/users", nil))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	expected := []daemon.UserResponseData{}
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *userSuite) TestUsersHasUser(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	u := mylog.Check2(auth.NewUser(st, auth.NewUserParams{
		Username:   "someuser",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}))
	st.Unlock()
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("GET", "/v2/users", nil))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	expected := []daemon.UserResponseData{
		{ID: u.ID, Username: u.Username, Email: u.Email},
	}
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *userSuite) testPostCreateUserFromAssertion(c *check.C, postData string, expectSudoer bool) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser, partnerUser, serialUser, badUser, badUserNoMatchingSerial, unknownUser})

	// mock the calls that create the user
	var deviceStateCreateUserCalled bool
	defer daemon.MockDeviceStateCreateUser(func(st *state.State, sudoer bool, email string, expiration time.Time) (*devicestate.CreatedUser, error) {
		deviceStateCreateUserCalled = true
		return nil, nil
	})()
	defer daemon.MockDeviceStateCreateKnownUsers(func(st *state.State, sudoer bool, email string) ([]*devicestate.CreatedUser, error) {
		c.Check(sudoer, check.Equals, expectSudoer)
		createdUsers := []*devicestate.CreatedUser{
			{
				Username: "goodserialguy",
			},
			{
				Username: "guy",
			},
			{
				Username: "partnerguy",
			},
		}
		return createdUsers, nil
	})()

	// do it!
	buf := bytes.NewBufferString(postData)
	req := mylog.Check2(http.NewRequest("POST", "/v2/create-user", buf))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	// note that we get a list here instead of a single
	// userResponseData item
	c.Check(rsp.Result, check.FitsTypeOf, []daemon.UserResponseData{})
	c.Check(deviceStateCreateUserCalled, check.Equals, false)
	seen := map[string]bool{}
	for _, u := range rsp.Result.([]daemon.UserResponseData) {
		seen[u.Username] = true
		c.Check(u, check.DeepEquals, daemon.UserResponseData{Username: u.Username})
	}
	c.Check(seen, check.DeepEquals, map[string]bool{
		"guy":           true,
		"partnerguy":    true,
		"goodserialguy": true,
	})
}

func (s *userSuite) TestPostCreateUserFromAssertionAllKnown(c *check.C) {
	expectSudoer := false
	s.testPostCreateUserFromAssertion(c, `{"known":true}`, expectSudoer)
}

func (s *userSuite) TestPostCreateUserFromAssertionAllAutomatic(c *check.C) {
	// automatic implies "sudoder"
	expectSudoer := true
	s.testPostCreateUserFromAssertion(c, `{"automatic":true}`, expectSudoer)
}
