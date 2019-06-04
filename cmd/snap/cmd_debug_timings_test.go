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
	"fmt"
	"net/http"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap"
)

type timingsCmdArgs struct {
	args, stdout, stderr, error string
}

var timingsTests = []timingsCmdArgs{{
	args:  "debug timings",
	error: "please provide change ID or type with --last=<type>, or query for --ensure=<name> or --startup=<name>",
}, {
	args:  "debug timings --ensure=seed 9",
	error: "cannot use change id, 'startup' or 'ensure' together",
}, {
	args:  "debug timings --ensure=seed --startup=ifacemgr",
	error: "cannot use change id, 'startup' or 'ensure' together",
}, {
	args:  "debug timings --last=install --all",
	error: "cannot use 'all' with change id or 'last'",
}, {
	args:  "debug timings --last=remove",
	error: `no changes of type "remove" found`,
}, {
	args:  "debug timings --startup=load-state 9",
	error: "cannot use change id, 'startup' or 'ensure' together",
}, {
	args:  "debug timings --all 9",
	error: "cannot use 'all' with change id or 'last'",
}, {
	args: "debug timings --last=install",
	stdout: "ID   Status        Doing      Undoing  Summary\n" +
		"40   Doing         910ms            -  task bar summary\n" +
		" ^                   1ms            -    foo summary\n" +
		"  ^                  1ms            -      bar summary\n\n",
}, {
	args: "debug timings 1",
	stdout: "ID   Status        Doing      Undoing  Summary\n" +
		"40   Doing         910ms            -  task bar summary\n" +
		" ^                   1ms            -    foo summary\n" +
		"  ^                  1ms            -      bar summary\n\n",
}, {
	args: "debug timings 1 --verbose",
	stdout: "ID   Status        Doing      Undoing  Label  Summary\n" +
		"40   Doing         910ms            -  bar    task bar summary\n" +
		" ^                   1ms            -  foo      foo summary\n" +
		"  ^                  1ms            -  bar        bar summary\n\n",
}, {
	args: "debug timings --ensure=seed",
	stdout: "ID    Status        Doing      Undoing  Summary\n" +
		"seed                    -            -  \n" +
		" ^                    8ms            -    baz summary\n" +
		"  ^                   8ms            -      booze summary\n" +
		"40    Doing         910ms            -  task bar summary\n" +
		" ^                    1ms            -    foo summary\n" +
		"  ^                   1ms            -      bar summary\n\n",
}, {
	args: "debug timings --ensure=seed --all",
	stdout: "ID    Status        Doing      Undoing  Summary\n" +
		"seed                    -            -  \n" +
		" ^                    8ms            -    bar summary 1\n" +
		" ^                    8ms            -    bar summary 2\n" +
		"40    Doing         910ms            -  task bar summary\n" +
		" ^                    1ms            -    foo summary\n" +
		"  ^                   1ms            -      bar summary\n\n",
}, {
	args: "debug timings --ensure=seed --all --verbose",
	stdout: "ID    Status        Doing      Undoing  Label  Summary\n" +
		"seed                    -            -         \n" +
		" ^                    8ms            -  abc      bar summary 1\n" +
		" ^                    8ms            -  abc      bar summary 2\n" +
		"40    Doing         910ms            -  bar    task bar summary\n" +
		" ^                    1ms            -  foo      foo summary\n" +
		"  ^                   1ms            -  bar        bar summary\n\n",
}, {
	args: "debug timings --startup=ifacemgr",
	stdout: "ID        Status        Doing      Undoing  Summary\n" +
		"ifacemgr                    -            -  \n" +
		" ^                        8ms            -    baz summary\n" +
		"  ^                       8ms            -      booze summary\n\n",
}, {
	args: "debug timings --startup=ifacemgr --all",
	stdout: "ID        Status        Doing      Undoing  Summary\n" +
		"ifacemgr                    -            -  \n" +
		" ^                        8ms            -    baz summary\n" +
		" ^                        9ms            -    baz summary\n\n",
}}

func (s *SnapSuite) TestGetDebugTimings(c *C) {
	s.mockCmdTimingsAPI(c)

	restore := main.MockIsStdinTTY(true)
	defer restore()

	for _, test := range timingsTests {
		s.stdout.Truncate(0)
		s.stderr.Truncate(0)

		c.Logf("Test: %s", test.args)

		_, err := main.Parser(main.Client()).ParseArgs(strings.Fields(test.args))
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(s.Stderr(), Equals, test.stderr)
			c.Check(s.Stdout(), Equals, test.stdout)
		}
	}
}

func (s *SnapSuite) mockCmdTimingsAPI(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")

		if r.URL.Path == "/v2/debug" {
			q := r.URL.Query()
			aspect := q.Get("aspect")
			c.Assert(aspect, Equals, "change-timings")

			changeID := q.Get("change-id")
			ensure := q.Get("ensure")
			startup := q.Get("startup")
			all := q.Get("all")

			switch {
			case changeID == "1":
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
				{"change-id":"1", "change-timings":{
					"40":{"doing-time":910000000,
						"doing-timings":[
							{"label":"foo", "summary": "foo summary", "duration": 1000001},
							{"level":1, "label":"bar", "summary": "bar summary", "duration": 1000002}
				]}}}]}`)
			case ensure == "seed" && all == "false":
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
					{"change-id":"1",
						"ensure-timings": [
								{"label":"baz", "summary": "baz summary", "duration": 8000001},
								{"level":1, "label":"booze", "summary": "booze summary", "duration": 8000002}
							],
						"change-timings":{
							"40":{"doing-time":910000000,
								"doing-timings":[
									{"label":"foo", "summary": "foo summary", "duration": 1000001},
									{"level":1, "label":"bar", "summary": "bar summary", "duration": 1000002}
					]}}}]}`)
			case ensure == "seed" && all == "true":
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
						{"change-id":"1",
							"ensure-timings": [
									{"label":"abc", "summary": "bar summary 1", "duration": 8000001},
									{"label":"abc", "summary": "bar summary 2", "duration": 8000002}
								],
							"change-timings":{
								"40":{"doing-time":910000000,
									"doing-timings":[
										{"label":"foo", "summary": "foo summary", "duration": 1000001},
										{"level":1, "label":"bar", "summary": "bar summary", "duration": 1000002}
						]}}}]}`)
			case startup == "ifacemgr" && all == "false":
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
					{"startup-timings": [
								{"label":"baz", "summary": "baz summary", "duration": 8000001},
								{"level":1, "label":"booze", "summary": "booze summary", "duration": 8000002}
					]}]}`)
			case startup == "ifacemgr" && all == "true":
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
					{"startup-timings": [
						{"label":"baz", "summary": "baz summary", "duration": 8000001},
						{"label":"baz", "summary": "baz summary", "duration": 9000001}
					]}]}`)
			default:
				c.Errorf("unexpected request: %s, %s, %s", changeID, ensure, all)
			}
			return
		}

		// request for all changes on --last=...
		if r.URL.Path == "/v2/changes" {
			fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[{
				"id":   "1",
				"kind": "install-snap",
				"summary": "a",
				"status": "Doing",
				"ready": false,
				"spawn-time": "2016-04-21T01:02:03Z",
				"ready-time": "2016-04-21T01:02:04Z",
				"tasks": [{"id":"99", "kind": "bar", "summary": ".", "status": "Doing", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
			  }]}`)
			return
		}

		// request for specific change
		if r.URL.Path == "/v2/changes/1" {
			fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{
				"id":   "1",
				"kind": "foo",
				"summary": "a",
				"status": "Doing",
				"ready": false,
				"spawn-time": "2016-04-21T01:02:03Z",
				"ready-time": "2016-04-21T01:02:04Z",
				"tasks": [{"id":"40", "kind": "bar", "summary": "task bar summary", "status": "Doing", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
			  }}`)
			return
		}

		c.Errorf("unexpected path %q", r.URL.Path)
	})
}
