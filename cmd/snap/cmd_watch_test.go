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
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/progress/progresstest"
	"github.com/snapcore/snapd/testutil"
)

var fmtWatchChangeJSON = `{"type": "sync", "result": {
  "id":   "two",
  "kind": "some-kind",
  "summary": "some summary...",
  "status": "Doing",
  "ready": false,
  "tasks": [{"id": "84", "kind": "bar", "summary": "some summary", "status": "Doing", "progress": {"label": "my-snap", "done": %d, "total": %d}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
}}`

func (s *SnapSuite) TestCmdWatch(c *C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	defer snap.MockMaxGoneTime(time.Millisecond)()
	defer snap.MockPollTime(time.Millisecond)()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes/two")
			fmt.Fprintf(w, fmtWatchChangeJSON, 0, 100*1024)
		case 2:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes/two")
			fmt.Fprintf(w, fmtWatchChangeJSON, 50*1024, 100*1024)
		case 3:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes/two")
			fmt.Fprintln(w, `{"type": "sync", "result": {"id": "two", "ready": true, "status": "Done"}}`)
		default:
			c.Errorf("expected 3 queries, currently on %d", n)
		}
	})

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"watch", "two"}))

	c.Assert(rest, HasLen, 0)
	c.Check(n, Equals, 3)
	c.Check(meter.Values, DeepEquals, []float64{51200})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestWatchLast(c *C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	defer snap.MockMaxGoneTime(time.Millisecond)()
	defer snap.MockPollTime(time.Millisecond)()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes")
			fmt.Fprintln(w, mockChangesJSON)
		case 2:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes/two")
			fmt.Fprintf(w, fmtWatchChangeJSON, 0, 100*1024)
		case 3:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes/two")
			fmt.Fprintf(w, fmtWatchChangeJSON, 50*1024, 100*1024)
		case 4:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes/two")
			fmt.Fprintln(w, `{"type": "sync", "result": {"id": "two", "ready": true, "status": "Done"}}`)
		default:
			c.Errorf("expected 4 queries, currently on %d", n)
		}
	})
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"watch", "--last=install"}))

	c.Assert(rest, HasLen, 0)
	c.Check(n, Equals, 4)
	c.Check(meter.Values, DeepEquals, []float64{51200})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestWatchLastQuestionmark(c *C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Check(r.Method, Equals, "GET")
		c.Assert(r.URL.Path, Equals, "/v2/changes")
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
		rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"watch", "--last=foobar?"}))

		c.Assert(rest, DeepEquals, []string{})
		c.Check(s.Stdout(), Matches, "")
		c.Check(s.Stderr(), Equals, "")

		_ = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"watch", "--last=foobar"}))
		if i == 0 {
			c.Assert(err, ErrorMatches, `no changes found`)
		} else {
			c.Assert(err, ErrorMatches, `no changes of type "foobar" found`)
		}
	}

	c.Check(n, Equals, 4)
}

func (s *SnapOpSuite) TestWatchWaitsForWaitTasks(c *C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			fmt.Fprintln(w, `{"type": "sync",
"result": {
"ready": false,
"status": "Doing",
"tasks": [{"kind": "bar", "summary": "...", "status": "Wait", "progress": {"done": 1, "total": 1}, "log": ["INFO: Task set to wait until a manual system restart allows to continue"]}]
}}`)
		case 1:
			fmt.Fprintln(w, `{"type": "sync",
"result": {
"ready": true,
"status": "Done",
"tasks": [{"kind": "bar", "summary": "...", "status": "Done", "progress": {"done": 1, "total": 1}, "log": ["INFO: Task set to wait until a manual system restart allows to continue"]}]
}}`)
		}

		n++
	})

	// snap watch will watch tasks in "Wait" state until they are done
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"watch", "x"}))

	c.Check(meter.Notices, testutil.Contains, "INFO: Task set to wait until a manual system restart allows to continue")
	c.Check(n, Equals, 2)
}
