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

package daemon_test

import (
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
)

type apiSuite struct {
	st *state.State
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) SetUpTest(c *check.C) {
	s.st = state.New(nil)
}

func (s *apiSuite) TestListIncludesAll(c *check.C) {
	// Very basic check to help stop us from not adding all the
	// commands to the command list.
	found := countCommandDecls(c, check.Commentf("TestListIncludesAll"))

	c.Check(found, check.Equals, len(daemon.APICommands()),
		check.Commentf(`At a glance it looks like you've not added all the Commands defined in api to the api list.`))
}

func (s *apiSuite) TestserFromRequestNoHeader(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	s.st.Lock()
	user := mylog.Check2(daemon.UserFromRequest(s.st, req))
	s.st.Unlock()

	c.Check(err, check.Equals, auth.ErrInvalidAuth)
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderNoMacaroons(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", "Invalid")

	s.st.Lock()
	user := mylog.Check2(daemon.UserFromRequest(s.st, req))
	s.st.Unlock()

	c.Check(err, check.ErrorMatches, "authorization header misses Macaroon prefix")
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderIncomplete(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root=""`)

	s.st.Lock()
	user := mylog.Check2(daemon.UserFromRequest(s.st, req))
	s.st.Unlock()

	c.Check(err, check.ErrorMatches, "invalid authorization header")
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderCorrectMissingUser(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	s.st.Lock()
	user := mylog.Check2(daemon.UserFromRequest(s.st, req))
	s.st.Unlock()

	c.Check(err, check.Equals, auth.ErrInvalidAuth)
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderValidUser(c *check.C) {
	s.st.Lock()
	expectedUser := mylog.Check2(auth.NewUser(s.st, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}))
	s.st.Unlock()
	c.Check(err, check.IsNil)

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, expectedUser.Macaroon))

	s.st.Lock()
	user := mylog.Check2(daemon.UserFromRequest(s.st, req))
	s.st.Unlock()

	c.Check(err, check.IsNil)
	c.Check(user, check.DeepEquals, expectedUser)
}

func (s *apiSuite) TestIsTrue(c *check.C) {
	form := &daemon.Form{}
	c.Check(daemon.IsTrue(form, "foo"), check.Equals, false)
	for _, f := range []string{"", "false", "0", "False", "f", "try"} {
		form.Values = map[string][]string{"foo": {f}}
		c.Check(daemon.IsTrue(form, "foo"), check.Equals, false, check.Commentf("expected %q to be false", f))
	}
	for _, t := range []string{"true", "1", "True", "t"} {
		form.Values = map[string][]string{"foo": {t}}
		c.Check(daemon.IsTrue(form, "foo"), check.Equals, true, check.Commentf("expected %q to be true", t))
	}
}
