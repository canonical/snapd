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

func (cs *clientSuite) TestClientAppCommonID(c *check.C) {
	expected := []*client.AppInfo{{
		Snap:     "foo",
		Name:     "foo",
		CommonID: "org.foo",
	}}
	buf, err := json.Marshal(expected)
	c.Assert(err, check.IsNil)
	cs.rsp = fmt.Sprintf(`{"type": "sync", "result": %s}`, buf)
	for _, chkr := range appcheckers {
		actual, err := chkr(cs, c)
		c.Assert(err, check.IsNil)
		c.Check(actual, check.DeepEquals, expected)
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

func (cs *clientSuite) TestClientServiceStart(c *check.C) {
	cs.rsp = `{"type": "async", "status-code": 202, "change": "24"}`

	type scenario struct {
		names   []string
		opts    client.StartOptions
		comment check.CommentInterface
	}

	var scenarios []scenario

	for _, names := range [][]string{
		nil, {},
		{"foo"},
		{"foo", "bar", "baz"},
	} {
		for _, opts := range []client.StartOptions{
			{Enable: true},
			{Enable: false},
		} {
			scenarios = append(scenarios, scenario{
				names:   names,
				opts:    opts,
				comment: check.Commentf("{%q; %#v}", names, opts),
			})
		}
	}

	for _, sc := range scenarios {
		id, err := cs.cli.Start(sc.names, sc.opts)
		if len(sc.names) == 0 {
			c.Check(id, check.Equals, "", sc.comment)
			c.Check(err, check.Equals, client.ErrNoNames, sc.comment)
			c.Check(cs.req, check.IsNil, sc.comment) // i.e. the request was never done
		} else {
			c.Assert(err, check.IsNil, sc.comment)
			c.Check(id, check.Equals, "24", sc.comment)
			c.Check(cs.req.URL.Path, check.Equals, "/v2/apps", sc.comment)
			c.Check(cs.req.Method, check.Equals, "POST", sc.comment)
			c.Check(cs.req.URL.Query(), check.HasLen, 0, sc.comment)

			inames := make([]interface{}, len(sc.names))
			for i, name := range sc.names {
				inames[i] = interface{}(name)
			}

			var reqOp map[string]interface{}
			c.Assert(json.NewDecoder(cs.req.Body).Decode(&reqOp), check.IsNil, sc.comment)
			if sc.opts.Enable {
				c.Check(len(reqOp), check.Equals, 3, sc.comment)
				c.Check(reqOp["enable"], check.Equals, true, sc.comment)
			} else {
				c.Check(len(reqOp), check.Equals, 2, sc.comment)
				c.Check(reqOp["enable"], check.IsNil, sc.comment)
			}
			c.Check(reqOp["action"], check.Equals, "start", sc.comment)
			c.Check(reqOp["names"], check.DeepEquals, inames, sc.comment)
		}
	}
}

func (cs *clientSuite) TestClientServiceStop(c *check.C) {
	cs.rsp = `{"type": "async", "status-code": 202, "change": "24"}`

	type tT struct {
		names   []string
		opts    client.StopOptions
		comment check.CommentInterface
	}

	var scs []tT

	for _, names := range [][]string{
		nil, {},
		{"foo"},
		{"foo", "bar", "baz"},
	} {
		for _, opts := range []client.StopOptions{
			{Disable: true},
			{Disable: false},
		} {
			scs = append(scs, tT{
				names:   names,
				opts:    opts,
				comment: check.Commentf("{%q; %#v}", names, opts),
			})
		}
	}

	for _, sc := range scs {
		id, err := cs.cli.Stop(sc.names, sc.opts)
		if len(sc.names) == 0 {
			c.Check(id, check.Equals, "", sc.comment)
			c.Check(err, check.Equals, client.ErrNoNames, sc.comment)
			c.Check(cs.req, check.IsNil, sc.comment) // i.e. the request was never done
		} else {
			c.Assert(err, check.IsNil, sc.comment)
			c.Check(id, check.Equals, "24", sc.comment)
			c.Check(cs.req.URL.Path, check.Equals, "/v2/apps", sc.comment)
			c.Check(cs.req.Method, check.Equals, "POST", sc.comment)
			c.Check(cs.req.URL.Query(), check.HasLen, 0, sc.comment)

			inames := make([]interface{}, len(sc.names))
			for i, name := range sc.names {
				inames[i] = interface{}(name)
			}

			var reqOp map[string]interface{}
			c.Assert(json.NewDecoder(cs.req.Body).Decode(&reqOp), check.IsNil, sc.comment)
			if sc.opts.Disable {
				c.Check(len(reqOp), check.Equals, 3, sc.comment)
				c.Check(reqOp["disable"], check.Equals, true, sc.comment)
			} else {
				c.Check(len(reqOp), check.Equals, 2, sc.comment)
				c.Check(reqOp["disable"], check.IsNil, sc.comment)
			}
			c.Check(reqOp["action"], check.Equals, "stop", sc.comment)
			c.Check(reqOp["names"], check.DeepEquals, inames, sc.comment)
		}
	}
}

func (cs *clientSuite) TestClientServiceRestart(c *check.C) {
	cs.rsp = `{"type": "async", "status-code": 202, "change": "24"}`

	type tT struct {
		names   []string
		opts    client.RestartOptions
		comment check.CommentInterface
	}

	var scs []tT

	for _, names := range [][]string{
		nil, {},
		{"foo"},
		{"foo", "bar", "baz"},
	} {
		for _, opts := range []client.RestartOptions{
			{Reload: true},
			{Reload: false},
		} {
			scs = append(scs, tT{
				names:   names,
				opts:    opts,
				comment: check.Commentf("{%q; %#v}", names, opts),
			})
		}
	}

	for _, sc := range scs {
		id, err := cs.cli.Restart(sc.names, sc.opts)
		if len(sc.names) == 0 {
			c.Check(id, check.Equals, "", sc.comment)
			c.Check(err, check.Equals, client.ErrNoNames, sc.comment)
			c.Check(cs.req, check.IsNil, sc.comment) // i.e. the request was never done
		} else {
			c.Assert(err, check.IsNil, sc.comment)
			c.Check(id, check.Equals, "24", sc.comment)
			c.Check(cs.req.URL.Path, check.Equals, "/v2/apps", sc.comment)
			c.Check(cs.req.Method, check.Equals, "POST", sc.comment)
			c.Check(cs.req.URL.Query(), check.HasLen, 0, sc.comment)

			inames := make([]interface{}, len(sc.names))
			for i, name := range sc.names {
				inames[i] = interface{}(name)
			}

			var reqOp map[string]interface{}
			c.Assert(json.NewDecoder(cs.req.Body).Decode(&reqOp), check.IsNil, sc.comment)
			if sc.opts.Reload {
				c.Check(len(reqOp), check.Equals, 3, sc.comment)
				c.Check(reqOp["reload"], check.Equals, true, sc.comment)
			} else {
				c.Check(len(reqOp), check.Equals, 2, sc.comment)
				c.Check(reqOp["reload"], check.IsNil, sc.comment)
			}
			c.Check(reqOp["action"], check.Equals, "restart", sc.comment)
			c.Check(reqOp["names"], check.DeepEquals, inames, sc.comment)
		}
	}
}
