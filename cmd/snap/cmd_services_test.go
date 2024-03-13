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
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
)

type appOpSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&appOpSuite{})

func (s *appOpSuite) SetUpTest(c *check.C) {
	s.BaseSnapSuite.SetUpTest(c)

	restoreClientRetry := client.MockDoTimings(time.Millisecond, time.Second)
	restorePollTime := snap.MockPollTime(time.Millisecond)
	s.AddCleanup(restoreClientRetry)
	s.AddCleanup(restorePollTime)
}

func (s *appOpSuite) TearDownTest(c *check.C) {
	s.BaseSnapSuite.TearDownTest(c)
}

func (s *appOpSuite) expectedBody(op string, names, extra []string) map[string]interface{} {
	inames := make([]interface{}, len(names))
	for i, name := range names {
		inames[i] = name
	}
	expectedBody := map[string]interface{}{
		"action": op,
		"names":  inames,
		"users":  []interface{}{},
	}
	for _, x := range extra {
		expectedBody[x] = true
	}
	return expectedBody
}

func (s *appOpSuite) args(op string, names, extra []string, noWait bool) []string {
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
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{op})
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

	_, err := snap.Parser(snap.Client()).ParseArgs(s.args(op, names, extra, noWait))
	c.Assert(err, check.ErrorMatches, "error")
	c.Check(n, check.Equals, 1)
}

func (s *appOpSuite) testOp(c *check.C, op, summary string, names, extra []string, noWait bool) {
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
	rest, err := snap.Parser(snap.Client()).ParseArgs(s.args(op, names, extra, noWait))
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

func (s *appOpSuite) TestAppOpsScopeSwitches(c *check.C) {
	var n int
	var body map[string]interface{}
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/apps")
			c.Check(r.URL.Query(), check.HasLen, 0)
			c.Check(r.Method, check.Equals, "POST")
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "change": "42", "status-code": 202}`)
			body = DecodedRequestBody(c, r)
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

	checkInvocation := func(op, summary string, names, args []string) map[string]interface{} {
		n = 0
		body = nil
		s.stdout.Reset()

		rest, err := snap.Parser(snap.Client()).ParseArgs(s.args(op, names, args, false))
		c.Assert(err, check.IsNil)
		c.Assert(rest, check.HasLen, 0)
		c.Check(s.Stderr(), check.Equals, "")
		expectedN := 3
		c.Check(s.Stdout(), check.Equals, summary+"\n")
		// ensure that the fake server api was actually hit
		c.Check(n, check.Equals, expectedN)
		return body
	}

	summaries := []string{"Started.", "Stopped.", "Restarted."}
	for i, op := range []string{"start", "stop", "restart"} {

		// Check without any scope options, that should default to empty
		// 'users' and no scope. This is the same as '--system --users'
		c.Check(checkInvocation(op, summaries[i], []string{"foo", "bar"}, nil), check.DeepEquals, map[string]interface{}{
			"action": op,
			"names":  []interface{}{"foo", "bar"},
			"users":  []interface{}{},
		})
		c.Check(checkInvocation(op, summaries[i], []string{"foo", "bar"}, []string{"user"}), check.DeepEquals, map[string]interface{}{
			"action": op,
			"names":  []interface{}{"foo", "bar"},
			"scope":  []interface{}{"user"},
			"users":  "self",
		})
		c.Check(checkInvocation(op, summaries[i], []string{"foo", "bar"}, []string{"users=all"}), check.DeepEquals, map[string]interface{}{
			"action": op,
			"names":  []interface{}{"foo", "bar"},
			"scope":  []interface{}{"user"},
			"users":  "all",
		})
		c.Check(checkInvocation(op, summaries[i], []string{"foo", "bar"}, []string{"users=all", "system"}), check.DeepEquals, map[string]interface{}{
			"action": op,
			"names":  []interface{}{"foo", "bar"},
			"users":  "all",
		})
		c.Check(checkInvocation(op, summaries[i], []string{"foo", "bar"}, []string{"system"}), check.DeepEquals, map[string]interface{}{
			"action": op,
			"names":  []interface{}{"foo", "bar"},
			"scope":  []interface{}{"system"},
			"users":  []interface{}{},
		})
	}
}

func (s *appOpSuite) TestAppOpsScopeInvalid(c *check.C) {
	checkInvocation := func(op string, names, args []string) error {
		rest, err := snap.Parser(snap.Client()).ParseArgs(s.args(op, names, args, false))
		c.Assert(rest, check.HasLen, len(names))
		return err
	}

	for _, op := range []string{"start", "stop", "restart"} {
		c.Check(checkInvocation(op, []string{"foo"}, []string{"user", "users=all"}), check.ErrorMatches, `--user and --users=all cannot be used in conjunction with each other`)
		c.Check(checkInvocation(op, []string{"bar"}, []string{"system", "user"}), check.ErrorMatches, `--system and --user cannot be used in conjunction with each other`)
		c.Check(checkInvocation(op, []string{"baz"}, []string{"users=my-user"}), check.ErrorMatches, `only "all" is supported as a value for --users`)
	}
}

func (s *appOpSuite) TestAppStatus(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/apps")
			c.Check(r.URL.Query(), check.HasLen, 1)
			c.Check(r.URL.Query().Get("select"), check.Equals, "service")
			c.Check(r.Method, check.Equals, "GET")
			w.WriteHeader(200)
			enc := json.NewEncoder(w)
			enc.Encode(map[string]interface{}{
				"type": "sync",
				"result": []map[string]interface{}{
					{
						"snap":         "foo",
						"name":         "bar",
						"daemon":       "oneshot",
						"daemon-scope": "system",
						"active":       false,
						"enabled":      true,
						"activators": []map[string]interface{}{
							{"name": "bar", "type": "timer", "active": true, "enabled": true},
						},
					}, {
						"snap":         "foo",
						"name":         "baz",
						"daemon":       "oneshot",
						"daemon-scope": "system",
						"active":       false,
						"enabled":      true,
						"activators": []map[string]interface{}{
							{"name": "baz-sock1", "type": "socket", "active": true, "enabled": true},
							{"name": "baz-sock2", "type": "socket", "active": false, "enabled": true},
						},
					}, {
						"snap":         "foo",
						"name":         "qux",
						"daemon":       "simple",
						"daemon-scope": "user",
						"active":       false,
						"enabled":      true,
					}, {
						"snap":    "foo",
						"name":    "zed",
						"active":  true,
						"enabled": true,
					},
				},
				"status":      "OK",
				"status-code": 200,
			})
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"services"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, `Service  Startup  Current   Notes
foo.bar  enabled  inactive  timer-activated
foo.baz  enabled  inactive  socket-activated
foo.qux  enabled  -         user
foo.zed  enabled  active    -
`)
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
}

func (s *appOpSuite) TestServiceCompletion(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/apps")
		c.Check(r.URL.Query(), check.HasLen, 1)
		c.Check(r.URL.Query().Get("select"), check.Equals, "service")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		enc := json.NewEncoder(w)
		enc.Encode(map[string]interface{}{
			"type": "sync",
			"result": []map[string]interface{}{
				{"snap": "a-snap", "name": "foo", "daemon": "simple"},
				{"snap": "a-snap", "name": "bar", "daemon": "simple"},
				{"snap": "b-snap", "name": "baz", "daemon": "simple"},
			},
			"status":      "OK",
			"status-code": 200,
		})

		n++
	})

	var comp = func(s string) string {
		comps := snap.ServiceName("").Complete(s)
		as := make([]string, len(comps))
		for i := range comps {
			as[i] = comps[i].Item
		}
		sort.Strings(as)
		return strings.Join(as, "  ")
	}

	c.Check(comp(""), check.Equals, "a-snap  a-snap.bar  a-snap.foo  b-snap.baz")
	c.Check(comp("a"), check.Equals, "a-snap  a-snap.bar  a-snap.foo")
	c.Check(comp("a-snap"), check.Equals, "a-snap  a-snap.bar  a-snap.foo")
	c.Check(comp("a-snap."), check.Equals, "a-snap.bar  a-snap.foo")
	c.Check(comp("a-snap.b"), check.Equals, "a-snap.bar")
	c.Check(comp("b"), check.Equals, "b-snap.baz")
	c.Check(comp("c"), check.Equals, "")

	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 7)
}

func (s *appOpSuite) TestAppStatusNoServices(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/apps")
			c.Check(r.URL.Query(), check.HasLen, 1)
			c.Check(r.URL.Query().Get("select"), check.Equals, "service")
			c.Check(r.Method, check.Equals, "GET")
			w.WriteHeader(200)
			enc := json.NewEncoder(w)
			enc.Encode(map[string]interface{}{
				"type":        "sync",
				"result":      []map[string]interface{}{},
				"status":      "OK",
				"status-code": 200,
			})
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"services"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "There are no services provided by installed snaps.\n")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
}

func (s *appOpSuite) TestLogsCommand(c *check.C) {
	n := 0
	timestamp := "2021-08-16T17:33:55Z"
	message := "Thing occurred\n"
	sid := "service1"
	pid := "1000"

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/logs")
			c.Check(r.Method, check.Equals, "GET")
			w.WriteHeader(200)
			_, err := w.Write([]byte{0x1E})
			c.Assert(err, check.IsNil)

			enc := json.NewEncoder(w)
			err = enc.Encode(map[string]interface{}{
				"timestamp": timestamp,
				"message":   message,
				"sid":       sid,
				"pid":       pid,
			})
			c.Assert(err, check.IsNil)

		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"logs", "snap"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	utcTime, err := time.Parse(time.RFC3339, timestamp)
	c.Assert(err, check.IsNil)
	localTime := utcTime.In(time.Local).Format(time.RFC3339)

	c.Check(s.Stdout(), check.Equals, fmt.Sprintf("%s %s[%s]: %s\n", localTime, sid, pid, message))
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
}

func (s *appOpSuite) TestLogsCommandWithAbsTimeFlag(c *check.C) {
	n := 0
	timestamp := "2021-08-16T17:33:55Z"
	message := "Thing occurred"
	sid := "service1"
	pid := "1000"

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/logs")
			c.Check(r.Method, check.Equals, "GET")
			w.WriteHeader(200)
			_, err := w.Write([]byte{0x1E})
			c.Assert(err, check.IsNil)

			enc := json.NewEncoder(w)
			err = enc.Encode(map[string]interface{}{
				"timestamp": timestamp,
				"message":   message,
				"sid":       sid,
				"pid":       pid,
			})
			c.Assert(err, check.IsNil)

		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"logs", "snap", "--abs-time"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	c.Check(s.Stdout(), check.Equals, fmt.Sprintf("%s %s[%s]: %s\n", timestamp, sid, pid, message))
	c.Check(s.Stderr(), check.Equals, "")

	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 1)
}
