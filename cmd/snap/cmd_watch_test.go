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

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/progress/progresstest"
)

var fmtWatchChangeJSON = `{"type": "sync", "result": {
  "id":   "42",
  "kind": "some-kind",
  "summary": "some summary...",
  "status": "Doing",
  "ready": false,
  "tasks": [{"id": "84", "kind": "bar", "summary": "some summary", "status": "Doing", "progress": {"label": "my-snap", "done": %d, "total": %d}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
}}`

func (s *SnapSuite) TestCmdWatch(c *C) {
	meter := &progresstest.Meter{}
	defer progress.MockMeter(meter)()
	restore := snap.MockMaxGoneTime(time.Millisecond)
	defer restore()

	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes/42")
			fmt.Fprintf(w, fmtWatchChangeJSON, 0, 100*1024)
		case 1:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes/42")
			fmt.Fprintf(w, fmtWatchChangeJSON, 50*1024, 100*1024)
		case 2:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"id": "42", "ready": true, "status": "Done"}}`)
		}
		n++
	})

	_, err := snap.Parser().ParseArgs([]string{"watch", "42"})
	c.Assert(err, IsNil)
	c.Check(n, Equals, 3)

	c.Check(meter.Values, DeepEquals, []float64{51200})
}
