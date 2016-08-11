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

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestCreateUser(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/create-user")
			var body map[string]string
			dec := json.NewDecoder(r.Body)
			err := dec.Decode(&body)
			c.Assert(err, check.IsNil)
			c.Check(body, check.DeepEquals, map[string]string{
				"email": "popper@lse.ac.uk",
			})
			fmt.Fprintln(w, `{"type": "sync", "result": {"username": "karl"}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})

	rest, err := snap.Parser().ParseArgs([]string{"create-user", "popper@lse.ac.uk"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(n, check.Equals, 1)
	c.Assert(s.Stdout(), check.Equals, `Created user "karl"`+"\n")
	c.Assert(s.Stderr(), check.Equals, "")
}
