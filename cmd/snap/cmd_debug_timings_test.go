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
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/cmd/snap"
)

type timingsCmdArgs struct {
	args, stdout, stderr, error string
}

var timingsTests = []timingsCmdArgs{
	{
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
			"40   Doing         910ms            -  lane 0 task bar summary\n" +
			" ^                   1ms            -    foo summary\n" +
			"  ^                  1ms            -      bar summary\n" +
			"41   Done          210ms            -  lane 1 task baz summary\n" +
			"42   Done          310ms            -  lane 1 task boo summary\n" +
			"43   Done          310ms            -  lane 0 task doh summary\n\n",
	}, {
		args: "debug timings 1",
		stdout: "ID   Status        Doing      Undoing  Summary\n" +
			"40   Doing         910ms            -  lane 0 task bar summary\n" +
			" ^                   1ms            -    foo summary\n" +
			"  ^                  1ms            -      bar summary\n" +
			"41   Done          210ms            -  lane 1 task baz summary\n" +
			"42   Done          310ms            -  lane 1 task boo summary\n" +
			"43   Done          310ms            -  lane 0 task doh summary\n\n",
	}, {
		args: "debug timings 1 --verbose",
		stdout: "ID   Status        Doing      Undoing  Label  Summary\n" +
			"40   Doing         910ms            -  bar    lane 0 task bar summary\n" +
			" ^                   1ms            -  foo      foo summary\n" +
			"  ^                  1ms            -  bar        bar summary\n" +
			"41   Done          210ms            -  baz    lane 1 task baz summary\n" +
			"42   Done          310ms            -  boo    lane 1 task boo summary\n" +
			"43   Done          310ms            -  doh    lane 0 task doh summary\n\n",
	}, {
		args: "debug timings --ensure=seed",
		stdout: "ID    Status        Doing      Undoing  Summary\n" +
			"seed                  8ms            -  \n" +
			" ^                    8ms            -    baz summary\n" +
			"  ^                   8ms            -      booze summary\n" +
			"40    Doing         910ms            -  task bar summary\n" +
			" ^                    1ms            -    foo summary\n" +
			"  ^                   1ms            -      bar summary\n\n",
	}, {
		args: "debug timings --ensure=seed --all",
		stdout: "ID    Status        Doing      Undoing  Summary\n" +
			"seed                  8ms            -  \n" +
			" ^                    8ms            -    bar summary 1\n" +
			" ^                    8ms            -    bar summary 2\n" +
			"40    Doing         910ms            -  task bar summary\n" +
			" ^                    1ms            -    foo summary\n" +
			"  ^                   1ms            -      bar summary\n" +
			"seed                  7ms            -  \n" +
			" ^                    7ms            -    baz summary 2\n" +
			"60    Doing         910ms            -  task bar summary\n" +
			" ^                    1ms            -    foo summary\n" +
			"  ^                   1ms            -      bar summary\n\n",
	}, {
		args: "debug timings --ensure=seed --all --verbose",
		stdout: "ID    Status        Doing      Undoing  Label  Summary\n" +
			"seed                  8ms            -         \n" +
			" ^                    8ms            -  abc      bar summary 1\n" +
			" ^                    8ms            -  abc      bar summary 2\n" +
			"40    Doing         910ms            -  bar    task bar summary\n" +
			" ^                    1ms            -  foo      foo summary\n" +
			"  ^                   1ms            -  bar        bar summary\n" +
			"seed                  7ms            -         \n" +
			" ^                    7ms            -  ghi      baz summary 2\n" +
			"60    Doing         910ms            -  bar    task bar summary\n" +
			" ^                    1ms            -  foo      foo summary\n" +
			"  ^                   1ms            -  bar        bar summary\n\n",
	}, {
		args: "debug timings --startup=ifacemgr",
		stdout: "ID        Status        Doing      Undoing  Summary\n" +
			"ifacemgr                  8ms            -  \n" +
			" ^                        8ms            -    baz summary\n" +
			"  ^                       8ms            -      booze summary\n\n",
	}, {
		args: "debug timings --startup=ifacemgr --all",
		stdout: "ID        Status        Doing      Undoing  Summary\n" +
			"ifacemgr                  8ms            -  \n" +
			" ^                        8ms            -    baz summary\n" +
			"ifacemgr                  9ms            -  \n" +
			" ^                        9ms            -    baz summary\n\n",
	}, {
		args: "debug timings 2",
		stdout: "ID   Status        Doing      Undoing  Summary\n" +
			"41   Undone            -        210ms  lane 0 task bar summary\n\n",
	},
}

func (s *SnapSuite) TestGetDebugTimings(c *C) {
	s.mockCmdTimingsAPI(c)

	restore := main.MockIsStdinTTY(true)
	defer restore()

	for _, test := range timingsTests {
		s.stdout.Truncate(0)
		s.stderr.Truncate(0)

		c.Logf("Test: %s", test.args)

		_ := mylog.Check2(main.Parser(main.Client()).ParseArgs(strings.Fields(test.args)))
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
				// lane 0 and lane 1 tasks, interleaved
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
				{"change-id":"1", "change-timings":{
					"41":{"doing-time":210000000, "status": "Done", "lane": 1, "ready-time": "2016-04-22T01:02:04Z", "kind": "baz", "summary": "lane 1 task baz summary"},
					"43":{"doing-time":310000000, "status": "Done", "ready-time": "2016-04-25T01:02:04Z", "kind": "doh", "summary": "lane 0 task doh summary"},
					"40":{"doing-time":910000000, "status": "Doing", "ready-time": "2016-04-20T00:00:00Z", "kind": "bar", "summary": "lane 0 task bar summary",
						"doing-timings":[
							{"label":"foo", "summary": "foo summary", "duration": 1000001},
							{"level":1, "label":"bar", "summary": "bar summary", "duration": 1000002}
						]},
					"42":{"doing-time":310000000, "status": "Done", "lane": 1, "ready-time": "2016-04-23T01:02:04Z", "kind": "boo", "summary": "lane 1 task boo summary"}
				}}]}`)
			case changeID == "2":
				// lane 0 tasks, interleaved
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
				{"change-id":"1", "change-timings":{
					"41":{"undoing-time":210000000, "status": "Undone", "lane": 0, "ready-time": "2016-04-22T01:02:04Z", "kind": "baz", "summary": "lane 0 task bar summary"}
				}}]}`)
			case ensure == "seed" && all == "false":
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
					{"change-id":"1",
					    "total-duration": 8000002,
						"ensure-timings": [
								{"label":"baz", "summary": "baz summary", "duration": 8000001},
								{"level":1, "label":"booze", "summary": "booze summary", "duration": 8000002}
							],
						"change-timings":{
							"40":{"doing-time":910000000, "status": "Doing", "kind": "bar", "summary": "task bar summary",
								"doing-timings":[
									{"label":"foo", "summary": "foo summary", "duration": 1000001},
									{"level":1, "label":"bar", "summary": "bar summary", "duration": 1000002}
					]}}}]}`)
			case ensure == "seed" && all == "true":
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
						{"change-id":"1",
							"total-duration": 8000002,
							"ensure-timings": [
									{"label":"abc", "summary": "bar summary 1", "duration": 8000001},
									{"label":"abc", "summary": "bar summary 2", "duration": 8000002}
								],
							"change-timings":{
								"40":{"doing-time":910000000, "status": "Doing", "kind": "bar", "summary": "task bar summary",
									"doing-timings":[
										{"label":"foo", "summary": "foo summary", "duration": 1000001},
										{"level":1, "label":"bar", "summary": "bar summary", "duration": 1000002}
								]}}},
						{"change-id":"2",
							"total-duration": 7000002,
							"ensure-timings": [{"label":"ghi", "summary": "baz summary 2", "duration": 7000002}],
							"change-timings":{
								"60":{"doing-time":910000000, "status": "Doing", "kind": "bar", "summary": "task bar summary",
									"doing-timings":[
										{"label":"foo", "summary": "foo summary", "duration": 1000001},
										{"level":1, "label":"bar", "summary": "bar summary", "duration": 1000002}
								]}}}]}`)
			case startup == "ifacemgr" && all == "false":
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
					{"total-duration": 8000002, "startup-timings": [
								{"label":"baz", "summary": "baz summary", "duration": 8000001},
								{"level":1, "label":"booze", "summary": "booze summary", "duration": 8000002}
					]}]}`)
			case startup == "ifacemgr" && all == "true":
				fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":[
					{"total-duration": 8000002, "startup-timings": [
						{"label":"baz", "summary": "baz summary", "duration": 8000001}
					]},
					{"total-duration": 9000002, "startup-timings": [
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
		c.Errorf("unexpected path %q", r.URL.Path)
	})
}

type TaskDef struct {
	TaskID    string
	Lane      int
	ReadyTime time.Time
}

func (s *SnapSuite) TestSortTimingsTasks(c *C) {
	mkTime := func(timeStr string) time.Time {
		t := mylog.Check2(time.Parse(time.RFC3339, timeStr))

		return t
	}

	testData := []struct {
		ChangeTimings map[string]main.ChangeTimings
		Expected      []string
	}{{
		// nothing to do
		ChangeTimings: map[string]main.ChangeTimings{},
		Expected:      []string{},
	}, {
		ChangeTimings: map[string]main.ChangeTimings{
			// tasks in lane 0 only
			"1": {ReadyTime: mkTime("2019-04-21T00:00:00Z")},
			"2": {ReadyTime: mkTime("2019-05-21T00:00:00Z")},
			"3": {ReadyTime: mkTime("2019-02-21T00:00:00Z")},
			"4": {ReadyTime: mkTime("2019-03-21T00:00:00Z")},
			"5": {ReadyTime: mkTime("2019-01-21T00:00:00Z")},
		},
		Expected: []string{"5", "3", "4", "1", "2"},
	}, {
		// task in lane 1 with a task in lane 0 before and after it
		ChangeTimings: map[string]main.ChangeTimings{
			"1": {Lane: 1, ReadyTime: mkTime("2019-01-21T00:00:00Z")},
			"2": {Lane: 0, ReadyTime: mkTime("2019-01-20T00:00:00Z")},
			"3": {Lane: 0, ReadyTime: mkTime("2019-01-22T00:00:00Z")},
		},
		Expected: []string{"2", "1", "3"},
	}, {
		// tasks in lane 1 only
		ChangeTimings: map[string]main.ChangeTimings{
			"1": {Lane: 1, ReadyTime: mkTime("2019-01-21T00:00:00Z")},
			"2": {Lane: 1, ReadyTime: mkTime("2019-01-20T00:00:00Z")},
			"3": {Lane: 1, ReadyTime: mkTime("2019-01-16T00:00:00Z")},
		},
		Expected: []string{"3", "2", "1"},
	}, {
		// tasks in lanes 0, 1, 2 with tasks from line 0 before and after lanes 1, 2
		ChangeTimings: map[string]main.ChangeTimings{
			"1": {Lane: 1, ReadyTime: mkTime("2019-01-21T00:00:00Z")},
			"2": {Lane: 0, ReadyTime: mkTime("2019-01-19T00:00:00Z")},
			"3": {Lane: 2, ReadyTime: mkTime("2019-01-20T00:00:00Z")},
			"4": {Lane: 0, ReadyTime: mkTime("2019-01-25T00:00:00Z")},
			"5": {Lane: 1, ReadyTime: mkTime("2019-01-20T00:00:00Z")},
			"6": {Lane: 2, ReadyTime: mkTime("2019-01-21T00:00:00Z")},
			"7": {Lane: 0, ReadyTime: mkTime("2019-01-18T00:00:00Z")},
			"8": {Lane: 0, ReadyTime: mkTime("2019-01-27T00:00:00Z")},
		},
		Expected: []string{"7", "2", "5", "1", "3", "6", "4", "8"},
	}, {
		// pathological case: lane 0 tasks have ready-time between lane 1 tasks
		ChangeTimings: map[string]main.ChangeTimings{
			"1": {Lane: 1, ReadyTime: mkTime("2019-01-20T00:00:00Z")},
			"2": {Lane: 1, ReadyTime: mkTime("2019-01-30T00:00:00Z")},
			"3": {Lane: 0, ReadyTime: mkTime("2019-01-27T00:00:00Z")},
			"4": {Lane: 0, ReadyTime: mkTime("2019-01-25T00:00:00Z")},
		},
		Expected: []string{"1", "2", "4", "3"},
	}}

	for _, data := range testData {
		tasks := main.SortTimingsTasks(data.ChangeTimings)
		c.Check(tasks, DeepEquals, data.Expected)
	}
}
