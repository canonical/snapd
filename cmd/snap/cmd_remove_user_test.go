// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	snap "github.com/snapcore/snapd/cmd/snap"
)

var removeUserJsonFmtReplyHappy = `{
  "type": "sync",
  "result": {
    "removed": [{"username": %q}]
  }
}`

var removeUserJsonReplyTooMany = `{
  "type": "sync",
  "result": {
    "removed": [{"username": "too"}, {"username": "many"}]
  }
}`

var removeUserJsonReplyTooFew = `{
  "type": "sync",
  "result": {
    "removed": []
  }
}`

func makeRemoveUserChecker(c *check.C, n *int, username string, fmtJsonReply string) func(w http.ResponseWriter, r *http.Request) {
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
				"username": username,
				"action":   "remove",
			}
			c.Check(gotBody, check.DeepEquals, wantBody)

			fmt.Fprint(w, fmtJsonReply)
		default:
			c.Fatalf("got too many requests (now on %d)", *n+1)
		}

		*n++
	}
	return f
}

func (s *SnapSuite) TestRemoveUser(c *check.C) {
	n := 0
	username := "karl"
	s.RedirectClientToTestServer(makeRemoveUserChecker(c, &n, username, fmt.Sprintf(removeUserJsonFmtReplyHappy, username)))

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"remove-user", "karl"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(n, check.Equals, 1)
	c.Assert(s.Stdout(), check.Equals, fmt.Sprintf("removed user %q\n", username))
	c.Assert(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestRemoveUserUnhappyTooMany(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(makeRemoveUserChecker(c, &n, "karl", removeUserJsonReplyTooMany))

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"remove-user", "karl"}))
	c.Assert(err, check.ErrorMatches, `internal error: RemoveUser returned unexpected number of removed users: 2`)
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestRemoveUserUnhappyTooFew(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(makeRemoveUserChecker(c, &n, "karl", removeUserJsonReplyTooFew))

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"remove-user", "karl"}))
	c.Assert(err, check.ErrorMatches, `internal error: RemoveUser returned unexpected number of removed users: 0`)
	c.Check(n, check.Equals, 1)
}
