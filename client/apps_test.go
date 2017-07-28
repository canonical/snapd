// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
)

func mksvc(snap, app string) *client.AppInfo {
	return &client.AppInfo{
		Snap:    snap,
		Name:    app,
		Daemon:  "simple",
		Active:  true,
		Enabled: true,
	}

}

func testClientApps(cs *clientSuite, c *check.C) ([]*client.AppInfo, error) {
	services, err := cs.cli.Apps([]string{"foo", "bar"}, client.AppOptions{})
	c.Check(cs.req.URL.Path, check.Equals, "/v2/apps")
	c.Check(cs.req.Method, check.Equals, "GET")
	query := cs.req.URL.Query()
	c.Check(query, check.HasLen, 1)
	c.Check(query.Get("names"), check.Equals, "foo,bar")

	return services, err
}

func testClientAppsService(cs *clientSuite, c *check.C) ([]*client.AppInfo, error) {
	services, err := cs.cli.Apps([]string{"foo", "bar"}, client.AppOptions{Service: true})
	c.Check(cs.req.URL.Path, check.Equals, "/v2/apps")
	c.Check(cs.req.Method, check.Equals, "GET")
	query := cs.req.URL.Query()
	c.Check(query, check.HasLen, 2)
	c.Check(query.Get("names"), check.Equals, "foo,bar")
	c.Check(query.Get("select"), check.Equals, "service")

	return services, err
}

var appcheckers = []func(*clientSuite, *check.C) ([]*client.AppInfo, error){testClientApps, testClientAppsService}

func (cs *clientSuite) TestClientServiceGetHappy(c *check.C) {
	expected := []*client.AppInfo{mksvc("foo", "foo"), mksvc("bar", "bar1")}
	buf, err := json.Marshal(expected)
	c.Assert(err, check.IsNil)
	cs.rsp = fmt.Sprintf(`{"type": "sync", "result": %s}`, buf)
	for _, chkr := range appcheckers {
		actual, err := chkr(cs, c)
		c.Assert(err, check.IsNil)
		c.Check(actual, check.DeepEquals, expected)
	}
}

func (cs *clientSuite) TestClientServiceGetSad(c *check.C) {
	cs.err = fmt.Errorf("xyzzy")
	for _, chkr := range appcheckers {
		actual, err := chkr(cs, c)
		c.Assert(err, check.ErrorMatches, ".* xyzzy")
		c.Check(actual, check.HasLen, 0)
	}
}

func testClientLogs(cs *clientSuite, c *check.C) ([]client.Log, error) {
	ch, err := cs.cli.Logs([]string{"foo", "bar"}, client.LogOptions{N: -1, Follow: false})
	c.Check(cs.req.URL.Path, check.Equals, "/v2/logs")
	c.Check(cs.req.Method, check.Equals, "GET")
	query := cs.req.URL.Query()
	c.Check(query, check.HasLen, 2)
	c.Check(query.Get("names"), check.Equals, "foo,bar")
	c.Check(query.Get("n"), check.Equals, "-1")

	var logs []client.Log
	if ch != nil {
		for log := range ch {
			logs = append(logs, log)
		}
	}

	return logs, err
}

func (cs *clientSuite) TestClientLogsHappy(c *check.C) {
	cs.rsp = `
{"message":"hello"}
{"message":"bye"}
`[1:] // remove the first \n

	logs, err := testClientLogs(cs, c)
	c.Assert(err, check.IsNil)
	c.Check(logs, check.DeepEquals, []client.Log{{Message: "hello"}, {Message: "bye"}})
}

func (cs *clientSuite) TestClientLogsDealsWithIt(c *check.C) {
	cs.rsp = `this is a line with no RS on it
this is a line with a RS after some junk{"message": "hello"}
{"message": "bye"}
and that was a regular line. The next one is empty, despite having a RS (and the one after is entirely empty):


`
	logs, err := testClientLogs(cs, c)
	c.Assert(err, check.IsNil)
	c.Check(logs, check.DeepEquals, []client.Log{{Message: "hello"}, {Message: "bye"}})
}

func (cs *clientSuite) TestClientLogsSad(c *check.C) {
	cs.err = fmt.Errorf("xyzzy")
	actual, err := testClientLogs(cs, c)
	c.Assert(err, check.ErrorMatches, ".* xyzzy")
	c.Check(actual, check.HasLen, 0)
}

func (cs *clientSuite) TestClientLogsOpts(c *check.C) {
	const (
		maxint = int((^uint(0)) >> 1)
		minint = -maxint - 1
	)
	for _, names := range [][]string{nil, {}, {"foo"}, {"foo", "bar"}} {
		for _, n := range []int{-1, 0, 1, minint, maxint} {
			for _, follow := range []bool{true, false} {
				iterdesc := check.Commentf("names: %v, n: %v, follow: %v", names, n, follow)

				ch, err := cs.cli.Logs(names, client.LogOptions{N: n, Follow: follow})
				c.Check(err, check.IsNil, iterdesc)
				c.Check(cs.req.URL.Path, check.Equals, "/v2/logs", iterdesc)
				c.Check(cs.req.Method, check.Equals, "GET", iterdesc)
				query := cs.req.URL.Query()
				numQ := 0

				var namesout []string
				if ns := query.Get("names"); ns != "" {
					namesout = strings.Split(ns, ",")
				}

				c.Check(len(namesout), check.Equals, len(names), iterdesc)
				if len(names) != 0 {
					c.Check(namesout, check.DeepEquals, names, iterdesc)
					numQ++
				}

				nout, nerr := strconv.Atoi(query.Get("n"))
				c.Check(nerr, check.IsNil, iterdesc)
				c.Check(nout, check.Equals, n, iterdesc)
				numQ++

				if follow {
					fout, ferr := strconv.ParseBool(query.Get("follow"))
					c.Check(fout, check.Equals, true, iterdesc)
					c.Check(ferr, check.IsNil, iterdesc)
					numQ++
				}

				c.Check(query, check.HasLen, numQ, iterdesc)

				for x := range ch {
					c.Logf("expecting empty channel, got %v during %s", x, iterdesc)
					c.Fail()
				}
			}
		}
	}
}
