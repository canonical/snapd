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

func (cs *clientSuite) TestClientSetConfigCallsEndpoint(c *check.C) {
	cs.cli.SetConfig("snap-name", map[string]interface{}{"key": "value"})
	c.Check(cs.req.Method, check.Equals, "PUT")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps/snap-name/config")
}

func (cs *clientSuite) TestClientGetConfigCallsEndpoint(c *check.C) {
	cs.cli.GetConfig("snap-name", "test-key")
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps/snap-name/config/test-key")
}

func (cs *clientSuite) TestClientSetConfig(c *check.C) {
	cs.rsp = `{
		"type": "async",
		"status-code": 202,
		"result": { },
		"change": "foo"
	}`
	id, err := cs.cli.SetConfig("snap-name", map[string]interface{}{"key": "value"})
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "foo")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"key": "value",
	})
}

func (cs *clientSuite) TestClientGetConfig(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": "test-value"
	}`
	value, err := cs.cli.GetConfig("snap-name", "test-key")
	c.Assert(err, check.IsNil)
	c.Check(value, check.Equals, "test-value")
}
