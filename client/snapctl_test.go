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

	"github.com/snapcore/snapd/client"

	"gopkg.in/check.v1"
)

func (cs *clientSuite) TestClientRunSnapctlCallsEndpoint(c *check.C) {
	options := client.SnapCtlOptions{
		Context: "1234ABCD",
		Args:    []string{"foo", "bar"},
	}
	cs.cli.RunSnapctl(options)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snapctl")
}

func (cs *clientSuite) TestClientRunSnapctl(c *check.C) {
	cs.rsp = `{
		"type": "sync",
        "status-code": 200,
		"result": {
			"stdout": "test stdout",
			"stderr": "test stderr"
		}
	}`

	options := client.SnapCtlOptions{
		Context: "1234ABCD",
		Args:    []string{"foo", "bar"},
	}

	stdout, stderr, err := cs.cli.RunSnapctl(options)
	c.Assert(err, check.IsNil)
	c.Check(string(stdout), check.Equals, "test stdout")
	c.Check(string(stderr), check.Equals, "test stderr")

	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"context": "1234ABCD",
		"args":    []interface{}{"foo", "bar"},
	})
}
