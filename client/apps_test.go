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
