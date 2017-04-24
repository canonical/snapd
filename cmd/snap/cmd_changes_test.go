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
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

var mockChangeJSON = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z",
  "tasks": [{"kind": "bar", "summary": "some summary", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
}}`

func (s *SnapSuite) TestChangeSimple(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if n < 2 {
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, mockChangeJSON)
		} else {
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	expectedChange := `(?ms)Status +Spawn +Ready +Summary
Do +2016-04-21T01:02:03Z +2016-04-21T01:02:04Z +some summary
`
	rest, err := snap.Parser().ParseArgs([]string{"change", "42"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, expectedChange)
	c.Check(s.Stderr(), check.Equals, "")

	rest, err = snap.Parser().ParseArgs([]string{"tasks", "42"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, expectedChange)
	c.Check(s.Stderr(), check.Equals, "")
}

var mockChangeInProgressJSON = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z",
  "tasks": [{"kind": "bar", "summary": "some summary", "status": "Doing", "progress": {"done": 50, "total": 100}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
}}`

func (s *SnapSuite) TestChangeProgress(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, mockChangeInProgressJSON)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"change", "42"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?ms)Status +Spawn +Ready +Summary
Doing +2016-04-21T01:02:03Z +2016-04-21T01:02:04Z +some summary \(50.00%\)
`)
	c.Check(s.Stderr(), check.Equals, "")
}
