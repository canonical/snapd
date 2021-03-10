// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type apiSuite struct {
	APIBaseSuite
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) TestUsersOnlyRoot(c *check.C) {
	for _, cmd := range api {
		if strings.Contains(cmd.Path, "user") {
			c.Check(cmd.RootOnly, check.Equals, true, check.Commentf(cmd.Path))
		}
	}
}

func (s *apiSuite) TestListIncludesAll(c *check.C) {
	// Very basic check to help stop us from not adding all the
	// commands to the command list.
	found := countCommandDecls(c, check.Commentf("TestListIncludesAll"))

	c.Check(found, check.Equals, len(api),
		check.Commentf(`At a glance it looks like you've not added all the Commands defined in api to the api list.`))
}

func (s *apiSuite) TestLoginUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(loginCmd, req, nil).(*resp)

	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)

	expected := userResponseData{
		ID:    1,
		Email: "email@.com",

		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}

	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
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

func (s *apiSuite) TestLoginUserWithUsername(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "username", "email": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(loginCmd, req, nil).(*resp)

	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)

	expected := userResponseData{
		ID:         1,
		Username:   "username",
		Email:      "email@.com",
		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
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

func (s *apiSuite) TestLoginUserNoEmailWithExistentLocalUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

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
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, localUser.Macaroon))

	rsp := loginUser(loginCmd, req, localUser).(*resp)

	expected := userResponseData{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
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

func (s *apiSuite) TestLoginUserWithExistentLocalUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

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
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, localUser.Macaroon))

	rsp := loginUser(loginCmd, req, localUser).(*resp)

	expected := userResponseData{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
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

func (s *apiSuite) TestLoginUserNewEmailWithExistentLocalUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

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
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, localUser.Macaroon))

	rsp := loginUser(loginCmd, req, localUser).(*resp)

	expected := userResponseData{
		ID:       1,
		Username: "username",
		Email:    "new.email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
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

func (s *apiSuite) TestLogoutUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/logout", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	rsp := logoutUser(logoutCmd, req, user).(*resp)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)

	state.Lock()
	_, err = auth.User(state, user.ID)
	state.Unlock()
	c.Check(err, check.Equals, auth.ErrInvalidUser)
}

func (s *apiSuite) TestLoginUserBadRequest(c *check.C) {
	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestLoginUserDeveloperAPIError(c *check.C) {
	s.daemon(c)

	s.err = fmt.Errorf("error-from-login-user")
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 401)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "error-from-login-user")
}

func (s *apiSuite) TestLoginUserTwoFactorRequiredError(c *check.C) {
	s.daemon(c)

	s.err = store.ErrAuthenticationNeeds2fa
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 401)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, client.ErrorKindTwoFactorRequired)
}

func (s *apiSuite) TestLoginUserTwoFactorFailedError(c *check.C) {
	s.daemon(c)

	s.err = store.Err2faFailed
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 401)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, client.ErrorKindTwoFactorFailed)
}

func (s *apiSuite) TestLoginUserInvalidCredentialsError(c *check.C) {
	s.daemon(c)

	s.err = store.ErrInvalidCredentials
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 401)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "invalid credentials")
}

func (s *apiSuite) TestUserFromRequestNoHeader(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.Equals, auth.ErrInvalidAuth)
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderNoMacaroons(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", "Invalid")

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.ErrorMatches, "authorization header misses Macaroon prefix")
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderIncomplete(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root=""`)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.ErrorMatches, "invalid authorization header")
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderCorrectMissingUser(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.Equals, auth.ErrInvalidAuth)
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderValidUser(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	expectedUser, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, expectedUser.Macaroon))

	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.IsNil)
	c.Check(user, check.DeepEquals, expectedUser)
}

func (s *apiSuite) TestIsTrue(c *check.C) {
	form := &multipart.Form{}
	c.Check(isTrue(form, "foo"), check.Equals, false)
	for _, f := range []string{"", "false", "0", "False", "f", "try"} {
		form.Value = map[string][]string{"foo": {f}}
		c.Check(isTrue(form, "foo"), check.Equals, false, check.Commentf("expected %q to be false", f))
	}
	for _, t := range []string{"true", "1", "True", "t"} {
		form.Value = map[string][]string{"foo": {t}}
		c.Check(isTrue(form, "foo"), check.Equals, true, check.Commentf("expected %q to be true", t))
	}
}

func (s *apiSuite) TestLogsNoServices(c *check.C) {
	// NOTE this is *apiSuite, not *appSuite, so there are no
	// installed snaps with services

	cmd := testutil.MockCommand(c, "systemctl", "").Also("journalctl", "")
	defer cmd.Restore()
	s.daemon(c)
	s.d.overlord.Loop()
	defer s.d.overlord.Stop()

	req, err := http.NewRequest("GET", "/v2/logs", nil)
	c.Assert(err, check.IsNil)

	rsp := getLogs(logsCmd, req, nil).(*resp)
	c.Assert(rsp.Status, check.Equals, 404)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
}
