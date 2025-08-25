// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) TestClientClusterAssemble(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
		"status-code": 202,
		"change": "42"
	}`

	opts := client.ClusterAssembleOptions{
		Secret:       "test-secret-123",
		Address:      "192.168.1.100:8080",
		ExpectedSize: 3,
	}

	changeID, err := cs.cli.ClusterAssemble(opts)
	c.Assert(err, check.IsNil)
	c.Check(changeID, check.Equals, "42")

	// verify the request
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/cluster")
	c.Check(cs.req.Header.Get("Content-Type"), check.Equals, "application/json")

	var reqBody map[string]interface{}
	err = json.NewDecoder(cs.req.Body).Decode(&reqBody)
	c.Assert(err, check.IsNil)
	c.Check(reqBody["action"], check.Equals, "assemble")
	c.Check(reqBody["secret"], check.Equals, "test-secret-123")
	c.Check(reqBody["address"], check.Equals, "192.168.1.100:8080")
	c.Check(reqBody["expected-size"], check.Equals, float64(3))
}

func (cs *clientSuite) TestClientClusterAssembleNoExpectedSize(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
		"status-code": 202,
		"change": "43"
	}`

	opts := client.ClusterAssembleOptions{
		Secret:  "test-secret-456",
		Address: "10.0.0.1:9090",
		// ExpectedSize defaults to 0
	}

	changeID, err := cs.cli.ClusterAssemble(opts)
	c.Assert(err, check.IsNil)
	c.Check(changeID, check.Equals, "43")

	var reqBody map[string]interface{}
	err = json.NewDecoder(cs.req.Body).Decode(&reqBody)
	c.Assert(err, check.IsNil)
	c.Check(reqBody["action"], check.Equals, "assemble")
	c.Check(reqBody["secret"], check.Equals, "test-secret-456")
	c.Check(reqBody["address"], check.Equals, "10.0.0.1:9090")
	// expected-size should be omitted when 0
	c.Check(reqBody["expected-size"], check.IsNil)
}

func (cs *clientSuite) TestClientClusterAssembleError(c *check.C) {
	cs.status = 400
	cs.rsp = `{
		"type": "error",
		"result": {
			"message": "invalid address format"
		}
	}`

	opts := client.ClusterAssembleOptions{
		Secret:  "test-secret",
		Address: "invalid-address",
	}

	_, err := cs.cli.ClusterAssemble(opts)
	c.Assert(err, check.ErrorMatches, "invalid address format")
}

func (cs *clientSuite) TestClientGetClusterUncommittedState(c *check.C) {
	cs.status = 200
	cs.rsp = `{
		"type": "sync",
		"result": {
			"cluster-id": "test-cluster-123",
			"devices": [
				{
					"id": 1,
					"brand-id": "canonical",
					"model": "ubuntu-core-24-amd64",
					"serial": "device-1",
					"addresses": ["192.168.1.10"]
				}
			],
			"subclusters": [
				{
					"name": "default",
					"devices": [1],
					"snaps": []
				}
			],
			"completed-at": "2024-01-15T10:30:00Z"
		}
	}`

	state, err := cs.cli.GetClusterUncommittedState()
	c.Assert(err, check.IsNil)
	c.Check(state.ClusterID, check.Equals, "test-cluster-123")
	c.Check(len(state.Devices), check.Equals, 1)
	c.Check(state.Devices[0].ID, check.Equals, 1)
	c.Check(state.Devices[0].BrandID, check.Equals, "canonical")
	c.Check(len(state.Subclusters), check.Equals, 1)
	c.Check(state.Subclusters[0].Name, check.Equals, "default")

	// verify the request
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/cluster/uncommitted")
}

func (cs *clientSuite) TestClientCommitClusterAssertion(c *check.C) {
	cs.status = 200
	cs.rsp = `{
		"type": "sync",
		"result": null
	}`

	// test the commit with cluster ID
	err := cs.cli.CommitClusterAssertion("test-cluster-456")
	c.Assert(err, check.IsNil)

	// verify the request
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/cluster/commit")
	c.Check(cs.req.Header.Get("Content-Type"), check.Equals, "application/json")

	var reqBody map[string]interface{}
	err = json.NewDecoder(cs.req.Body).Decode(&reqBody)
	c.Assert(err, check.IsNil)
	c.Check(reqBody["cluster-id"], check.Equals, "test-cluster-456")
}
