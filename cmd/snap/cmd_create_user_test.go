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

package main_test

import (
	"encoding/json"
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func makeCreateUserChecker(c *check.C, n *int, email string, sudoer, known bool) func(w http.ResponseWriter, r *http.Request) {
	f := func(w http.ResponseWriter, r *http.Request) {
		switch *n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/users")
			var gotBody map[string]interface{}
			dec := json.NewDecoder(r.Body)
			mylog.Check(dec.Decode(&gotBody))
			c.Assert(err, check.IsNil)

			wantBody := map[string]interface{}{
				"action": "create",
			}
			if email != "" {
				wantBody["email"] = "one@email.com"
			}
			if sudoer {
				wantBody["sudoer"] = true
			}
			if known {
				wantBody["known"] = true
			}
			c.Check(gotBody, check.DeepEquals, wantBody)

			if email == "" {
				fmt.Fprintln(w, `{"type": "sync", "result": [{"username": "karl", "ssh-keys": ["a","b"]}]}`)
			} else {
				fmt.Fprintln(w, `{"type": "sync", "result": [{"username": "karl", "ssh-keys": ["a","b"]}]}`)
			}
		default:
			c.Fatalf("got too many requests (now on %d)", *n+1)
		}

		*n++
	}
	return f
}

func (s *SnapSuite) TestCreateUserNoSudoer(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(makeCreateUserChecker(c, &n, "one@email.com", false, false))

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-user", "one@email.com"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(n, check.Equals, 1)
	c.Assert(s.Stdout(), check.Equals, `created user "karl"`+"\n")
	c.Assert(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestCreateUserSudoer(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(makeCreateUserChecker(c, &n, "one@email.com", true, false))

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-user", "--sudoer", "one@email.com"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(n, check.Equals, 1)
	c.Assert(s.Stdout(), check.Equals, `created user "karl"`+"\n")
	c.Assert(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestCreateUserJSON(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(makeCreateUserChecker(c, &n, "one@email.com", false, false))

	expectedResponse := &client.CreateUserResult{
		Username: "karl",
		SSHKeys:  []string{"a", "b"},
	}
	actualResponse := &client.CreateUserResult{}

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-user", "--json", "one@email.com"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(n, check.Equals, 1)
	json.Unmarshal(s.stdout.Bytes(), actualResponse)
	c.Assert(actualResponse, check.DeepEquals, expectedResponse)
	c.Assert(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestCreateUserNoEmailJSON(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(makeCreateUserChecker(c, &n, "", false, true))

	expectedResponse := []*client.CreateUserResult{{
		Username: "karl",
		SSHKeys:  []string{"a", "b"},
	}}
	var actualResponse []*client.CreateUserResult

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-user", "--json", "--known"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(n, check.Equals, 1)
	json.Unmarshal(s.stdout.Bytes(), &actualResponse)
	c.Assert(actualResponse, check.DeepEquals, expectedResponse)
	c.Assert(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestCreateUserKnown(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(makeCreateUserChecker(c, &n, "one@email.com", false, true))

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-user", "--known", "one@email.com"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestCreateUserKnownNoEmail(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(makeCreateUserChecker(c, &n, "", false, true))

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-user", "--known"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(n, check.Equals, 1)
}
