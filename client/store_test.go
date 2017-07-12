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

	"gopkg.in/check.v1"
)

func (cs *clientSuite) TestSetStore(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": {
			"url": "http://example.com/"
		}
	}`

	storeURL, err := cs.cli.SetStore("store-1")
	c.Assert(err, check.IsNil)

	c.Check(storeURL, check.Equals, "http://example.com/")

	c.Check(cs.req.Method, check.Equals, "PUT")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/store")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Assert(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"store": "store-1",
	})
}

func (cs *clientSuite) TestSetStoreError(c *check.C) {
	cs.rsp = `{
		"type": "error",
		"status-code": 400,
		"result": {
			"message": "a-message"
		}
	}`

	_, err := cs.cli.SetStore("store-1")
	c.Assert(err, check.NotNil)
	c.Check(err, check.ErrorMatches, "a-message")
}

func (cs *clientSuite) TestUnsetStore(c *check.C) {
	cs.rsp = `{
		"type": "sync"
	}`
	err := cs.cli.UnsetStore()
	c.Assert(err, check.IsNil)

	c.Check(cs.req.Method, check.Equals, "DELETE")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/store")
}

func (cs *clientSuite) TestUnsetStoreError(c *check.C) {
	cs.rsp = `{
		"type": "error",
		"status-code": 400,
		"result": {
			"message": "a-message"
		}
	}`

	err := cs.cli.UnsetStore()
	c.Assert(err, check.NotNil)
	c.Check(err, check.ErrorMatches, "a-message")
}
