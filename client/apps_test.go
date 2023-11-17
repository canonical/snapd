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

	// logs cannot have a deadline because of "-f"
	_, ok := cs.req.Context().Deadline()
	c.Check(ok, check.Equals, false)

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

func (cs *clientSuite) TestClientLogsNotFound(c *check.C) {
	cs.rsp = `{"type":"error","status-code":404,"status":"Not Found","result":{"message":"snap \"foo\" not found","kind":"snap-not-found","value":"foo"}}`
	cs.status = 404
	actual, err := testClientLogs(cs, c)
	c.Assert(err, check.ErrorMatches, `snap "foo" not found`)
	c.Check(actual, check.HasLen, 0)
}

func (cs *clientSuite) checkCommonFields(c *check.C, reqOp map[string]interface{}, names, scope, users []string, comment check.CommentInterface) {
	inames := make([]interface{}, len(names))
	for i, name := range names {
		inames[i] = interface{}(name)
	}

	c.Check(reqOp["names"], check.DeepEquals, inames, comment)
	if len(scope) > 0 {
		snames := make([]interface{}, len(scope))
		for i, scope := range scope {
			snames[i] = interface{}(scope)
		}
		c.Check(reqOp["scope"], check.DeepEquals, snames, comment)
	} else {
		c.Check(reqOp["scope"], check.IsNil, comment)
	}
	if len(users) > 0 {
		unames := make([]interface{}, len(users))
		for i, u := range users {
			unames[i] = interface{}(u)
		}
		c.Check(reqOp["user-services-of"], check.DeepEquals, unames, comment)
	} else {
		c.Check(reqOp["user-services-of"], check.IsNil, comment)
	}
}

func (cs *clientSuite) TestClientServiceStart(c *check.C) {
	cs.status = 202
	cs.rsp = `{"type": "async", "status-code": 202, "change": "24"}`

	tests := []struct {
		names []string
		scope []string
		users []string
		opts  client.StartOptions
	}{
		{},
		{
			opts: client.StartOptions{
				Enable: true,
			},
		},
		{
			names: []string{"foo"},
		},
		{
			names: []string{"foo"},
			opts: client.StartOptions{
				Enable: true,
			},
		},
		{
			names: []string{"foo", "bar", "baz"},
		},
		{
			names: []string{"foo", "bar", "baz"},
			opts: client.StartOptions{
				Enable: true,
			},
		},
		{
			names: []string{"foo"},
			scope: []string{"user"},
		},
		{
			names: []string{"foo"},
			scope: []string{"system"},
		},
		{
			names: []string{"foo"},
			scope: []string{"system", "user"},
		},
		{
			names: []string{"foo"},
			users: []string{"user"},
		},
		{
			names: []string{"foo"},
			users: []string{"users"},
		},
	}

	for _, sc := range tests {
		comment := check.Commentf("{%q; %q; %q; %#v}", sc.names, sc.scope, sc.users, sc.opts)
		id, err := cs.cli.Start(sc.names, sc.scope, sc.users, sc.opts)
		if len(sc.names) == 0 {
			c.Check(id, check.Equals, "", comment)
			c.Check(err, check.Equals, client.ErrNoNames, comment)
			c.Check(cs.req, check.IsNil, comment) // i.e. the request was never done
		} else {
			c.Assert(err, check.IsNil, comment)
			c.Check(id, check.Equals, "24", comment)
			c.Check(cs.req.URL.Path, check.Equals, "/v2/apps", comment)
			c.Check(cs.req.Method, check.Equals, "POST", comment)
			c.Check(cs.req.URL.Query(), check.HasLen, 0, comment)

			var reqOp map[string]interface{}
			c.Assert(json.NewDecoder(cs.req.Body).Decode(&reqOp), check.IsNil, comment)
			c.Check(reqOp["action"], check.Equals, "start", comment)
			cs.checkCommonFields(c, reqOp, sc.names, sc.scope, sc.users, comment)
			if sc.opts.Enable {
				c.Check(reqOp["enable"], check.Equals, true, comment)
			} else {
				c.Check(reqOp["enable"], check.IsNil, comment)
			}
		}
	}
}

func (cs *clientSuite) TestClientServiceStop(c *check.C) {
	cs.status = 202
	cs.rsp = `{"type": "async", "status-code": 202, "change": "24"}`

	tests := []struct {
		names []string
		scope []string
		users []string
		opts  client.StopOptions
	}{
		{},
		{
			opts: client.StopOptions{
				Disable: true,
			},
		},
		{
			names: []string{"foo"},
		},
		{
			names: []string{"foo"},
			opts: client.StopOptions{
				Disable: true,
			},
		},
		{
			names: []string{"foo", "bar", "baz"},
		},
		{
			names: []string{"foo", "bar", "baz"},
			opts: client.StopOptions{
				Disable: true,
			},
		},
		{
			names: []string{"foo"},
			scope: []string{"user"},
		},
		{
			names: []string{"foo"},
			scope: []string{"system"},
		},
		{
			names: []string{"foo"},
			scope: []string{"system", "user"},
		},
		{
			names: []string{"foo"},
			users: []string{"user"},
		},
		{
			names: []string{"foo"},
			users: []string{"users"},
		},
	}

	for _, sc := range tests {
		comment := check.Commentf("{%q; %q; %q; %#v}", sc.names, sc.scope, sc.users, sc.opts)
		id, err := cs.cli.Stop(sc.names, sc.scope, sc.users, sc.opts)
		if len(sc.names) == 0 {
			c.Check(id, check.Equals, "", comment)
			c.Check(err, check.Equals, client.ErrNoNames, comment)
			c.Check(cs.req, check.IsNil, comment) // i.e. the request was never done
		} else {
			c.Assert(err, check.IsNil, comment)
			c.Check(id, check.Equals, "24", comment)
			c.Check(cs.req.URL.Path, check.Equals, "/v2/apps", comment)
			c.Check(cs.req.Method, check.Equals, "POST", comment)
			c.Check(cs.req.URL.Query(), check.HasLen, 0, comment)

			var reqOp map[string]interface{}
			c.Assert(json.NewDecoder(cs.req.Body).Decode(&reqOp), check.IsNil, comment)
			c.Check(reqOp["action"], check.Equals, "stop", comment)
			cs.checkCommonFields(c, reqOp, sc.names, sc.scope, sc.users, comment)
			if sc.opts.Disable {
				c.Check(reqOp["disable"], check.Equals, true, comment)
			} else {
				c.Check(reqOp["disable"], check.IsNil, comment)
			}
		}
	}
}

func (cs *clientSuite) TestClientServiceRestart(c *check.C) {
	cs.status = 202
	cs.rsp = `{"type": "async", "status-code": 202, "change": "24"}`

	tests := []struct {
		names []string
		scope []string
		users []string
		opts  client.RestartOptions
	}{
		{},
		{
			opts: client.RestartOptions{
				Reload: true,
			},
		},
		{
			names: []string{"foo"},
		},
		{
			names: []string{"foo"},
			opts: client.RestartOptions{
				Reload: true,
			},
		},
		{
			names: []string{"foo", "bar", "baz"},
		},
		{
			names: []string{"foo", "bar", "baz"},
			opts: client.RestartOptions{
				Reload: true,
			},
		},
		{
			names: []string{"foo"},
			scope: []string{"user"},
		},
		{
			names: []string{"foo"},
			scope: []string{"system"},
		},
		{
			names: []string{"foo"},
			scope: []string{"system", "user"},
		},
		{
			names: []string{"foo"},
			users: []string{"user"},
		},
		{
			names: []string{"foo"},
			users: []string{"users"},
		},
	}

	for _, sc := range tests {
		comment := check.Commentf("{%q; %q; %q; %#v}", sc.names, sc.scope, sc.users, sc.opts)
		id, err := cs.cli.Restart(sc.names, sc.scope, sc.users, sc.opts)
		if len(sc.names) == 0 {
			c.Check(id, check.Equals, "", comment)
			c.Check(err, check.Equals, client.ErrNoNames, comment)
			c.Check(cs.req, check.IsNil, comment) // i.e. the request was never done
		} else {
			c.Assert(err, check.IsNil, comment)
			c.Check(id, check.Equals, "24", comment)
			c.Check(cs.req.URL.Path, check.Equals, "/v2/apps", comment)
			c.Check(cs.req.Method, check.Equals, "POST", comment)
			c.Check(cs.req.URL.Query(), check.HasLen, 0, comment)

			var reqOp map[string]interface{}
			c.Assert(json.NewDecoder(cs.req.Body).Decode(&reqOp), check.IsNil, comment)
			cs.checkCommonFields(c, reqOp, sc.names, sc.scope, sc.users, comment)
			c.Check(reqOp["action"], check.Equals, "restart", comment)
			if sc.opts.Reload {
				c.Check(reqOp["reload"], check.Equals, true, comment)
			} else {
				c.Check(reqOp["reload"], check.IsNil, comment)
			}
		}
	}
}
