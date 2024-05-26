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
	"encoding/json"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) TestClientAliasCallsEndpoint(c *check.C) {
	cs.cli.Alias("alias-snap", "cmd1", "alias1")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/aliases")
}

func (cs *clientSuite) TestClientAlias(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "chgid"
	}`
	id := mylog.Check2(cs.cli.Alias("alias-snap", "cmd1", "alias1"))
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "chgid")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	mylog.Check(decoder.Decode(&body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "alias",
		"snap":   "alias-snap",
		"app":    "cmd1",
		"alias":  "alias1",
	})
}

func (cs *clientSuite) TestClientUnaliasCallsEndpoint(c *check.C) {
	cs.cli.Unalias("alias1")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/aliases")
}

func (cs *clientSuite) TestClientUnalias(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "chgid"
	}`
	id := mylog.Check2(cs.cli.Unalias("alias1"))
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "chgid")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	mylog.Check(decoder.Decode(&body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "unalias",
		"snap":   "alias1",
		"alias":  "alias1",
	})
}

func (cs *clientSuite) TestClientDisableAllAliasesCallsEndpoint(c *check.C) {
	cs.cli.DisableAllAliases("some-snap")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/aliases")
}

func (cs *clientSuite) TestClientDisableAllAliases(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "chgid"
	}`
	id := mylog.Check2(cs.cli.DisableAllAliases("some-snap"))
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "chgid")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	mylog.Check(decoder.Decode(&body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "unalias",
		"snap":   "some-snap",
	})
}

func (cs *clientSuite) TestClientRemoveManualAliasCallsEndpoint(c *check.C) {
	cs.cli.RemoveManualAlias("alias1")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/aliases")
}

func (cs *clientSuite) TestClientRemoveManualAlias(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "chgid"
	}`
	id := mylog.Check2(cs.cli.RemoveManualAlias("alias1"))
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "chgid")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	mylog.Check(decoder.Decode(&body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "unalias",
		"alias":  "alias1",
	})
}

func (cs *clientSuite) TestClientPreferCallsEndpoint(c *check.C) {
	cs.cli.Prefer("some-snap")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/aliases")
}

func (cs *clientSuite) TestClientPrefer(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "chgid"
	}`
	id := mylog.Check2(cs.cli.Prefer("some-snap"))
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "chgid")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	mylog.Check(decoder.Decode(&body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "prefer",
		"snap":   "some-snap",
	})
}

func (cs *clientSuite) TestClientAliasesCallsEndpoint(c *check.C) {
	_, _ = cs.cli.Aliases()
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/aliases")
}

func (cs *clientSuite) TestClientAliases(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
                    "foo": {
                        "foo0": {"command": "foo", "status": "auto", "auto": "foo"},
                        "foo_reset": {"command": "foo.reset", "manual": "reset", "status": "manual"}
                    },
                    "bar": {
                        "bar_dump": {"command": "bar.dump", "status": "manual", "manual": "dump"},
                        "bar_dump.1": {"command": "bar.dump", "status": "disabled", "auto": "dump"}
                    }
		}
	}`
	allStatuses := mylog.Check2(cs.cli.Aliases())
	c.Assert(err, check.IsNil)
	c.Check(allStatuses, check.DeepEquals, map[string]map[string]client.AliasStatus{
		"foo": {
			"foo0":      {Command: "foo", Status: "auto", Auto: "foo"},
			"foo_reset": {Command: "foo.reset", Status: "manual", Manual: "reset"},
		},
		"bar": {
			"bar_dump":   {Command: "bar.dump", Status: "manual", Manual: "dump"},
			"bar_dump.1": {Command: "bar.dump", Status: "disabled", Auto: "dump"},
		},
	})
}
