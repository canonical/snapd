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

	snap "github.com/snapcore/snapd/cmd/snap"
)

func makeRemoveUserChecker(c *check.C, n *int, username string) func(w http.ResponseWriter, r *http.Request) {
	f := func(w http.ResponseWriter, r *http.Request) {
		switch *n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/users")
			var gotBody map[string]interface{}
			dec := json.NewDecoder(r.Body)
			err := dec.Decode(&gotBody)
			c.Assert(err, check.IsNil)

			wantBody := map[string]interface{}{
				"username": username,
				"action":   "remove",
			}
			c.Check(gotBody, check.DeepEquals, wantBody)

			fmt.Fprintf(w, `{
  "type": "sync",
  "result": {
    "removed": [{"username": %q}]
  }
}
`, username)
		default:
			c.Fatalf("got too many requests (now on %d)", *n+1)
		}

		*n++
	}
	return f
}

func (s *SnapSuite) TestRemoveUser(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(makeRemoveUserChecker(c, &n, "karl"))

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"remove-user", "karl"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(n, check.Equals, 1)
	c.Assert(s.Stdout(), check.Equals, `removed user "karl"`+"\n")
	c.Assert(s.Stderr(), check.Equals, "")
}
