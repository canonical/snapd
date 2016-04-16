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
	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
	"io/ioutil"
	"time"
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

func (cs *clientSuite) TestClientChangeError(c *check.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Error",
  "ready": true,
  "tasks": [{"kind": "bar", "summary": "...", "status": "Error", "progress": {"done": 1, "total": 1}, "log": ["ERROR: something broke"]}],
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
			Kind:     "bar",
			Summary:  "...",
			Status:   "Error",
			Progress: client.TaskProgress{Done: 1, Total: 1},
			Log:      []string{"ERROR: something broke"},
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

	for _, i := range []client.ChangeSelector{client.ChangesAll, client.ChangesReady, client.ChangesInProgress} {
		chg, err := cs.cli.Changes(i)
		c.Assert(err, check.IsNil)
		c.Check(chg, check.DeepEquals, []*client.Change{{
			ID:      "uno",
			Kind:    "foo",
			Summary: "...",
			Status:  "Do",
			Tasks:   []*client.Task{{Kind: "bar", Summary: "...", Status: "Do", Progress: client.TaskProgress{Done: 0, Total: 1}}},
		}})
		c.Check(cs.req.URL.RawQuery, check.Equals, "select="+i.String())
	}
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

	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)

	c.Assert(string(body), check.Equals, "{\"action\":\"abort\"}\n")
}
