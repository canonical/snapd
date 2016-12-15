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
)

func (cs *clientSuite) TestClientAliasCallsEndpoint(c *check.C) {
	cs.cli.Alias("alias-snap", []string{"alias1", "alias2"})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/aliases")
}

func (cs *clientSuite) TestClientAlias(c *check.C) {
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "chgid"
	}`
	id, err := cs.cli.Alias("alias-snap", []string{"alias1", "alias2"})
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "chgid")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action":  "alias",
		"snap":    "alias-snap",
		"aliases": []interface{}{"alias1", "alias2"},
	})
}

func (cs *clientSuite) TestClientUnaliasCallsEndpoint(c *check.C) {
	cs.cli.Unalias("alias-snap", []string{"alias1", "alias2"})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/aliases")
}

func (cs *clientSuite) TestClientUnalias(c *check.C) {
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "chgid"
	}`
	id, err := cs.cli.Unalias("alias-snap", []string{"alias1", "alias2"})
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "chgid")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action":  "unalias",
		"snap":    "alias-snap",
		"aliases": []interface{}{"alias1", "alias2"},
	})
}
