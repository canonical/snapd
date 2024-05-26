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

package client_test

import (
	"encoding/json"
	"time"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) testWarnings(c *check.C, all bool) {
	t1 := time.Date(2018, 9, 19, 12, 41, 18, 505007495, time.UTC)
	t2 := time.Date(2018, 9, 19, 12, 44, 19, 680362867, time.UTC)
	cs.rsp = `{
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
		"type": "sync",
		"warning-count": 2,
		"warning-timestamp": "2018-09-19T12:44:19.680362867Z"
	}`

	ws := mylog.Check2(cs.cli.Warnings(client.WarningsOptions{All: all}))
	c.Assert(err, check.IsNil)
	c.Check(ws, check.DeepEquals, []*client.Warning{
		{
			Message:     "hello world number one",
			FirstAdded:  t1,
			LastAdded:   t1,
			ExpireAfter: time.Hour * 24 * 28,
			RepeatAfter: time.Hour * 24,
		},
		{
			Message:     "hello world number two",
			FirstAdded:  t2,
			LastAdded:   t2,
			ExpireAfter: time.Hour * 24 * 28,
			RepeatAfter: time.Hour * 24,
		},
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/warnings")
	query := cs.req.URL.Query()
	if all {
		c.Check(query, check.HasLen, 1)
		c.Check(query.Get("select"), check.Equals, "all")
	} else {
		c.Check(query, check.HasLen, 0)
	}

	// this could be done at the end of any sync method
	count, stamp := cs.cli.WarningsSummary()
	c.Check(count, check.Equals, 2)
	c.Check(stamp, check.Equals, t2)
}

func (cs *clientSuite) TestWarningsAll(c *check.C) {
	cs.testWarnings(c, true)
}

func (cs *clientSuite) TestWarnings(c *check.C) {
	cs.testWarnings(c, false)
}

func (cs *clientSuite) TestOkay(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": { }
	}`
	t0 := time.Now()
	mylog.Check(cs.cli.Okay(t0))
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Query(), check.HasLen, 0)
	var body map[string]interface{}
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
	c.Check(body, check.HasLen, 2)
	c.Check(body["action"], check.Equals, "okay")
	c.Check(body["timestamp"], check.Equals, t0.Format(time.RFC3339Nano))

	// note there's no warnings summary in the response
	count, stamp := cs.cli.WarningsSummary()
	c.Check(count, check.Equals, 0)
	c.Check(stamp, check.Equals, time.Time{})
}
