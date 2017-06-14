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

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/systemd"
)

func mksvc(snap, app string) client.Service {
	return client.Service{
		Snap: snap,
		AppInfo: client.AppInfo{
			Name:   app,
			Daemon: "simple",
		},
		ServiceStatus: &systemd.ServiceStatus{
			ServiceFileName: fmt.Sprintf("snap.%s.%s.service", snap, app),
			LoadState:       "loaded",
			ActiveState:     "active",
			SubState:        "running",
			UnitFileState:   "enabled",
		},
	}

}

func testClientServiceStatus(cs *clientSuite, c *check.C) ([]client.Service, error) {
	services, err := cs.cli.ServiceStatus([]string{"foo", "bar"})
	c.Check(cs.req.URL.Path, check.Equals, "/v2/services")
	c.Check(cs.req.Method, check.Equals, "GET")
	query := cs.req.URL.Query()
	c.Check(query, check.HasLen, 1)
	c.Check(query.Get("services"), check.Equals, "foo,bar")

	return services, err
}

func testClientServiceLogs(cs *clientSuite, c *check.C) ([]client.Service, error) {
	services, err := cs.cli.ServiceLogs([]string{"foo", "bar"})
	c.Check(cs.req.URL.Path, check.Equals, "/v2/services")
	c.Check(cs.req.Method, check.Equals, "GET")
	query := cs.req.URL.Query()
	c.Check(query, check.HasLen, 2)
	c.Check(query.Get("services"), check.Equals, "foo,bar")
	withLogs, _ := strconv.ParseBool(query.Get("logs"))
	c.Check(withLogs, check.Equals, true)

	return services, err
}

var getcheckers = []func(*clientSuite, *check.C) ([]client.Service, error){
	testClientServiceStatus,
	testClientServiceLogs,
}

func (cs *clientSuite) TestClientServiceGetHappy(c *check.C) {
	expected := []client.Service{mksvc("foo", "foo"), mksvc("bar", "bar1")}
	buf, err := json.Marshal(expected)
	c.Assert(err, check.IsNil)
	cs.rsp = fmt.Sprintf(`{"type": "sync", "result": %s}`, buf)
	for _, checker := range getcheckers {
		actual, err := checker(cs, c)
		c.Assert(err, check.IsNil)
		c.Check(actual, check.DeepEquals, expected)
	}
}

func (cs *clientSuite) TestClientServiceGetSad(c *check.C) {
	cs.err = fmt.Errorf("xyzzy")
	for _, checker := range getcheckers {
		actual, err := checker(cs, c)
		c.Assert(err, check.ErrorMatches, ".* xyzzy")
		c.Check(actual, check.HasLen, 0)
	}
}

func (cs *clientSuite) TestClientServiceOp(c *check.C) {
	cs.rsp = `{"type": "async", "status-code": 202, "change": "24"}`
	id, err := cs.cli.ServiceOp("an-action", []string{"foo", "bar"})
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "24")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/services")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Query(), check.HasLen, 0)

	var svcOp client.ServiceOp
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&svcOp), check.IsNil)
	c.Check(svcOp, check.DeepEquals, client.ServiceOp{
		Services: []string{"foo", "bar"},
		Action:   "an-action",
	})
}
