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

package client_test

import (
	"io"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) TestClientChange(c *check.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z",
  "tasks": [{"kind": "bar", "summary": "...", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
}}`

	chg, err := cs.cli.Change("uno")
	c.Assert(err, check.IsNil)
	c.Check(chg, check.DeepEquals, &client.Change{
		ID:      "uno",
		Kind:    "foo",
		Summary: "...",
		Status:  "Do",
		Tasks: []*client.Task{{
			Kind:      "bar",
			Summary:   "...",
			Status:    "Do",
			Progress:  client.TaskProgress{Done: 0, Total: 1},
			SpawnTime: time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC),
			ReadyTime: time.Date(2016, 04, 21, 1, 2, 4, 0, time.UTC),
		}},

		SpawnTime: time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC),
		ReadyTime: time.Date(2016, 04, 21, 1, 2, 4, 0, time.UTC),
	})
}

func (cs *clientSuite) TestClientChangeData(c *check.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "data": {"n": 42}
}}`

	chg, err := cs.cli.Change("uno")
	c.Assert(err, check.IsNil)
	var n int
	err = chg.Get("n", &n)
	c.Assert(err, check.IsNil)
	c.Assert(n, check.Equals, 42)

	err = chg.Get("missing", &n)
	c.Assert(err, check.Equals, client.ErrNoData)
}

func (cs *clientSuite) TestClientChangeRestartingState(c *check.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false
},
 "maintenance": {"kind": "system-restart", "message": "system is restarting"}
}`

	chg, err := cs.cli.Change("uno")
	c.Check(chg, check.NotNil)
	c.Check(chg.ID, check.Equals, "uno")
	c.Check(err, check.IsNil)
	c.Check(cs.cli.Maintenance(), check.ErrorMatches, `system is restarting`)
}

func (cs *clientSuite) TestClientChangeError(c *check.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Error",
  "ready": true,
  "tasks": [{"kind": "bar", "summary": "...", "status": "Error", "progress": {"done": 1, "total": 1}, "log": ["ERROR: something broke"], "snap-name": "a_snap", "instance-name": "instance_value", "revision": "891"}],
  "err": "error message"
}}`

	chg, err := cs.cli.Change("uno")
	c.Assert(err, check.IsNil)
	c.Check(chg, check.DeepEquals, &client.Change{
		ID:      "uno",
		Kind:    "foo",
		Summary: "...",
		Status:  "Error",
		Tasks: []*client.Task{{
			Kind:         "bar",
			Summary:      "...",
			Status:       "Error",
			Progress:     client.TaskProgress{Done: 1, Total: 1},
			Log:          []string{"ERROR: something broke"},
			SnapName:     "a_snap",
			InstanceName: "instance_value",
			Revision:     "891",
		}},
		Err:   "error message",
		Ready: true,
	})
}

func (cs *clientSuite) TestClientChangesString(c *check.C) {
	for k, v := range map[client.ChangeSelector]string{
		client.ChangesAll:        "all",
		client.ChangesReady:      "ready",
		client.ChangesInProgress: "in-progress",
	} {
		c.Check(k.String(), check.Equals, v)
	}
}

func (cs *clientSuite) TestClientChanges(c *check.C) {
	cs.rsp = `{"type": "sync", "result": [{
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "tasks": [{"kind": "bar", "summary": "...", "status": "Do", "progress": {"done": 0, "total": 1}}]
}]}`

	for _, i := range []*client.ChangesOptions{
		{Selector: client.ChangesAll},
		{Selector: client.ChangesReady},
		{Selector: client.ChangesInProgress},
		{SnapName: "foo"},
		nil,
	} {
		chg, err := cs.cli.Changes(i)
		c.Assert(err, check.IsNil)
		c.Check(chg, check.DeepEquals, []*client.Change{{
			ID:      "uno",
			Kind:    "foo",
			Summary: "...",
			Status:  "Do",
			Tasks:   []*client.Task{{Kind: "bar", Summary: "...", Status: "Do", Progress: client.TaskProgress{Done: 0, Total: 1}}},
		}})
		if i == nil {
			c.Check(cs.req.URL.RawQuery, check.Equals, "")
		} else {
			if i.Selector != 0 {
				c.Check(cs.req.URL.RawQuery, check.Equals, "select="+i.Selector.String())
			} else {
				c.Check(cs.req.URL.RawQuery, check.Equals, "for="+i.SnapName)
			}
		}
	}

}

func (cs *clientSuite) TestClientChangesData(c *check.C) {
	cs.rsp = `{"type": "sync", "result": [{
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "data": {"n": 42}
}]}`

	chgs, err := cs.cli.Changes(&client.ChangesOptions{Selector: client.ChangesAll})
	c.Assert(err, check.IsNil)

	chg := chgs[0]
	var n int
	err = chg.Get("n", &n)
	c.Assert(err, check.IsNil)
	c.Assert(n, check.Equals, 42)

	err = chg.Get("missing", &n)
	c.Assert(err, check.Equals, client.ErrNoData)
}

func (cs *clientSuite) TestClientAbort(c *check.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Hold",
  "ready": true,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z"
}}`

	chg, err := cs.cli.Abort("uno")
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(chg, check.DeepEquals, &client.Change{
		ID:      "uno",
		Kind:    "foo",
		Summary: "...",
		Status:  "Hold",
		Ready:   true,

		SpawnTime: time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC),
		ReadyTime: time.Date(2016, 04, 21, 1, 2, 4, 0, time.UTC),
	})

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Assert(string(body), check.Equals, "{\"action\":\"abort\"}\n")
}
