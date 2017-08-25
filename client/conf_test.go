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

func (cs *clientSuite) TestClientSetConfCallsEndpoint(c *check.C) {
	cs.cli.SetConf("snap-name", map[string]interface{}{"key": "value"})
	c.Check(cs.req.Method, check.Equals, "PUT")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps/snap-name/conf")
}

func (cs *clientSuite) TestClientGetConfCallsEndpoint(c *check.C) {
	cs.cli.Conf("snap-name", []string{"test-key"})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps/snap-name/conf")
	c.Check(cs.req.URL.Query().Get("keys"), check.Equals, "test-key")
}

func (cs *clientSuite) TestClientGetConfCallsEndpointMultipleKeys(c *check.C) {
	cs.cli.Conf("snap-name", []string{"test-key1", "test-key2"})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps/snap-name/conf")
	c.Check(cs.req.URL.Query().Get("keys"), check.Equals, "test-key1,test-key2")
}

func (cs *clientSuite) TestClientSetConf(c *check.C) {
	cs.rsp = `{
		"type": "async",
		"status-code": 202,
		"result": { },
		"change": "foo"
	}`
	id, err := cs.cli.SetConf("snap-name", map[string]interface{}{"key": "value"})
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

func (cs *clientSuite) TestClientGetConf(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": {"test-key": "test-value"}
	}`
	value, err := cs.cli.Conf("snap-name", []string{"test-key"})
	c.Assert(err, check.IsNil)
	c.Check(value, check.DeepEquals, map[string]interface{}{"test-key": "test-value"})
}

func (cs *clientSuite) TestClientGetConfBigInt(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": {"test-key": 1234567890}
	}`
	value, err := cs.cli.Conf("snap-name", []string{"test-key"})
	c.Assert(err, check.IsNil)
	c.Check(value, check.DeepEquals, map[string]interface{}{"test-key": json.Number("1234567890")})
}

func (cs *clientSuite) TestClientGetConfMultipleKeys(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": {
			"test-key1": "test-value1",
			"test-key2": "test-value2"
		}
	}`
	value, err := cs.cli.Conf("snap-name", []string{"test-key1", "test-key2"})
	c.Assert(err, check.IsNil)
	c.Check(value, check.DeepEquals, map[string]interface{}{
		"test-key1": "test-value1",
		"test-key2": "test-value2",
	})
}
