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
	"io"
	"net/http"
	"time"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

type warningSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&warningSuite{})

const twoWarnings = `{
			"result": [
			    {
				"expire-after": "672h0m0s",
				"first-added": "2018-09-19T12:41:18.505007495Z",
				"last-added": "2018-09-19T12:41:18.505007495Z",
				"message": "hello world number one",
				"repeat-after": "24h0m0s"
			    },
			    {
				"expire-after": "672h0m0s",
				"first-added": "2018-09-19T12:44:19.680362867Z",
				"last-added": "2018-09-19T12:44:19.680362867Z",
				"message": "hello world number two",
				"repeat-after": "24h0m0s"
			    }
			],
			"status": "OK",
			"status-code": 200,
			"type": "sync"
		}`

func mkWarningsFakeHandler(c *check.C, body string) func(w http.ResponseWriter, r *http.Request) {
	var called bool
	return func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v2/warnings")
		c.Check(r.URL.Query(), check.HasLen, 0)

		buf, err := io.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Check(string(buf), check.Equals, "")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func (s *warningSuite) TestNoWarningsEver(c *check.C) {
	s.RedirectClientToTestServer(mkWarningsFakeHandler(c, `{"type": "sync", "status-code": 200, "result": []}`))

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"warnings", "--abs-time"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "No warnings.\n")
}

func (s *warningSuite) TestNoFurtherWarnings(c *check.C) {
	snap.WriteWarningTimestamp(time.Now())

	s.RedirectClientToTestServer(mkWarningsFakeHandler(c, `{"type": "sync", "status-code": 200, "result": []}`))

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"warnings", "--abs-time"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "No further warnings.\n")
}

func (s *warningSuite) TestWarnings(c *check.C) {
	s.RedirectClientToTestServer(mkWarningsFakeHandler(c, twoWarnings))

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"warnings", "--abs-time", "--unicode=never"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, `
last-occurrence:  2018-09-19T12:41:18Z
warning: |
  hello world number one
---
last-occurrence:  2018-09-19T12:44:19Z
warning: |
  hello world number two
`[1:])
}

func (s *warningSuite) TestVerboseWarnings(c *check.C) {
	s.RedirectClientToTestServer(mkWarningsFakeHandler(c, twoWarnings))

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"warnings", "--abs-time", "--verbose", "--unicode=never"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, `
first-occurrence:  2018-09-19T12:41:18Z
last-occurrence:   2018-09-19T12:41:18Z
acknowledged:      --
repeats-after:     1d00h
expires-after:     28d0h
warning: |
  hello world number one
---
first-occurrence:  2018-09-19T12:44:19Z
last-occurrence:   2018-09-19T12:44:19Z
acknowledged:      --
repeats-after:     1d00h
expires-after:     28d0h
warning: |
  hello world number two
`[1:])
}

func (s *warningSuite) TestOkay(c *check.C) {
	t0 := time.Now()
	snap.WriteWarningTimestamp(t0)

	var n int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n != 1 {
			c.Fatalf("expected 1 request, now on %d", n)
		}
		c.Check(r.URL.Path, check.Equals, "/v2/warnings")
		c.Check(r.URL.Query(), check.HasLen, 0)
		c.Assert(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{"action": "okay", "timestamp": t0.Format(time.RFC3339Nano)})
		c.Check(r.Method, check.Equals, "POST")
		w.WriteHeader(200)
		fmt.Fprintln(w, `{
			"status": "OK",
			"status-code": 200,
			"type": "sync"
		}`)
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"okay"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *warningSuite) TestOkayBeforeWarnings(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"okay"})
	c.Assert(err, check.ErrorMatches, "you must have looked at the warnings before acknowledging them. Try 'snap warnings'.")
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *warningSuite) TestListWithWarnings(c *check.C) {
	var called bool
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")
		c.Check(r.URL.Query(), check.HasLen, 0)

		buf, err := io.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Check(string(buf), check.Equals, "")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		fmt.Fprintln(w, `{
				"result": [{}],
				"status": "OK",
				"status-code": 200,
				"type": "sync",
				"warning-count": 2,
				"warning-timestamp": "2018-09-19T12:44:19.680362867Z"
			}`)
	})
	cli := snap.Client()
	rest, err := snap.Parser(cli).ParseArgs([]string{"list"})
	c.Assert(err, check.IsNil)

	{
		// TODO: I hope to get to refactor run() so we can
		// call it from tests and not have to do this (whole
		// block) by hand

		count, stamp := cli.WarningsSummary()
		c.Check(count, check.Equals, 2)
		c.Check(stamp, check.Equals, time.Date(2018, 9, 19, 12, 44, 19, 680362867, time.UTC))

		snap.MaybePresentWarnings(count, stamp)
	}

	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `
Name  Version  Rev    Tracking  Publisher  Notes
      -        unset  -         -          disabled
`[1:])
	c.Check(s.Stderr(), check.Equals, "WARNING: There are 2 new warnings. See 'snap warnings'.\n")

}
