// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestAbortLast(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes")
			fmt.Fprintln(w, mockChangesJSON)
		case 2:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/two")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{"action": "abort"})
			fmt.Fprintln(w, mockChangeJSON)
		default:
			c.Errorf("expected 2 queries, currently on %d", n)
		}
	})
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"abort", "--last=install"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")

	c.Assert(n, check.Equals, 2)
}

func (s *SnapSuite) TestAbortLastQuestionmark(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Check(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v2/changes")
		switch n {
		case 1, 2:
			fmt.Fprintln(w, `{"type": "sync", "result": []}`)
		case 3, 4:
			fmt.Fprintln(w, mockChangesJSON)
		default:
			c.Errorf("expected 4 calls, now on %d", n)
		}
	})
	for i := 0; i < 2; i++ {
		rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"abort", "--last=foobar?"}))
		c.Assert(err, check.IsNil)
		c.Assert(rest, check.DeepEquals, []string{})
		c.Check(s.Stdout(), check.Matches, "")
		c.Check(s.Stderr(), check.Equals, "")

		_ = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"abort", "--last=foobar"}))
		if i == 0 {
			c.Assert(err, check.ErrorMatches, `no changes found`)
		} else {
			c.Assert(err, check.ErrorMatches, `no changes of type "foobar" found`)
		}
	}

	c.Check(n, check.Equals, 4)
}
