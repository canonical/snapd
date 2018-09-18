// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
)

type appOpSuite struct {
	BaseSnapSuite

	restoreAll func()
}

var _ = check.Suite(&appOpSuite{})

func (s *appOpSuite) SetUpTest(c *check.C) {
	s.BaseSnapSuite.SetUpTest(c)

	restoreClientRetry := client.MockDoRetry(time.Millisecond, 10*time.Millisecond)
	restorePollTime := snap.MockPollTime(time.Millisecond)
	s.restoreAll = func() {
		restoreClientRetry()
		restorePollTime()
	}
}

func (s *appOpSuite) TearDownTest(c *check.C) {
	s.restoreAll()
	s.BaseSnapSuite.TearDownTest(c)
}

func (s *appOpSuite) expectedBody(op string, names []string, extra []string) map[string]interface{} {
	inames := make([]interface{}, len(names))
	for i, name := range names {
		inames[i] = name
	}
	expectedBody := map[string]interface{}{
		"action": op,
		"names":  inames,
	}
	for _, x := range extra {
		expectedBody[x] = true
	}
	return expectedBody
}

func (s *appOpSuite) args(op string, names []string, extra []string, noWait bool) []string {
	args := []string{op}
	if noWait {
		args = append(args, "--no-wait")
	}
	for _, x := range extra {
		args = append(args, "--"+x)
	}
	args = append(args, names...)
	return args
}

func (s *appOpSuite) testOpNoArgs(c *check.C, op string) {
	s.RedirectClientToTestServer(nil)
	_, err := snap.Parser().ParseArgs([]string{op})
	c.Assert(err, check.ErrorMatches, `.* required argument .* not provided`)
}

func (s *appOpSuite) testOpErrorResponse(c *check.C, op string, names []string, extra []string, noWait bool) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/apps")
			c.Check(r.URL.Query(), check.HasLen, 0)
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, s.expectedBody(op, names, extra))
			w.WriteHeader(400)
			fmt.Fprintln(w, `{"type": "error", "result": {"message": "error"}, "status-code": 400}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})

	_, err := snap.Parser().ParseArgs(s.args(op, names, extra, noWait))
	c.Assert(err, check.ErrorMatches, "error")
	c.Check(n, check.Equals, 1)
}

func (s *appOpSuite) testOp(c *check.C, op, summary string, names []string, extra []string, noWait bool) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/apps")
			c.Check(r.URL.Query(), check.HasLen, 0)
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, s.expectedBody(op, names, extra))
			c.Check(r.Method, check.Equals, "POST")
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "change": "42", "status-code": 202}`)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "Doing"}}`)
		case 2:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)
		default:
			c.Fatalf("expected to get 2 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs(s.args(op, names, extra, noWait))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	expectedN := 3
	if noWait {
		summary = "42"
		expectedN = 1
	}
	c.Check(s.Stdout(), check.Equals, summary+"\n")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, expectedN)
}

func (s *appOpSuite) TestAppOps(c *check.C) {
	extras := []string{"enable", "disable", "reload"}
	summaries := []string{"Started.", "Stopped.", "Restarted."}
	for i, op := range []string{"start", "stop", "restart"} {
		s.testOpNoArgs(c, op)
		for _, extra := range [][]string{nil, {extras[i]}} {
			for _, noWait := range []bool{false, true} {
				for _, names := range [][]string{
					{"foo"},
					{"foo", "bar"},
					{"foo", "bar.baz"},
				} {
					s.testOpErrorResponse(c, op, names, extra, noWait)
					s.testOp(c, op, summaries[i], names, extra, noWait)
					s.stdout.Reset()
				}
			}
		}
	}
}
