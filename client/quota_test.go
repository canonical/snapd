// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"bytes"
	"encoding/json"
	"io/ioutil"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/jsonutil"
)

func (cs *clientSuite) TestCreateQuotaGroupInvalidName(c *check.C) {
	_, err := cs.cli.EnsureQuota("", "", nil, nil)
	c.Check(err, check.ErrorMatches, `cannot create or update quota group without a name`)
}

func (cs *clientSuite) TestEnsureQuotaGroup(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
		"status-code": 202,
		"change": "42"
	}`

	quotaValues := &client.QuotaValues{
		Memory: quantity.Size(1001),
		CPU: &client.QuotaCPUValues{
			Count:      1,
			Percentage: 50,
		},
		CPUSet: &client.QuotaCPUSetValues{
			CPUs: []int{0},
		},
		Threads: 32,
	}

	chgID, err := cs.cli.EnsureQuota("foo", "bar", []string{"snap-a", "snap-b"}, quotaValues)
	c.Assert(err, check.IsNil)
	c.Assert(chgID, check.Equals, "42")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/quotas")
	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	err = jsonutil.DecodeWithNumber(bytes.NewReader(body), &req)
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action":     "ensure",
		"group-name": "foo",
		"parent":     "bar",
		"snaps":      []interface{}{"snap-a", "snap-b"},
		"constraints": map[string]interface{}{
			"memory": json.Number("1001"),
			"cpu": map[string]interface{}{
				"count":      json.Number("1"),
				"percentage": json.Number("50"),
			},
			"cpu-set": map[string]interface{}{
				"cpus": []interface{}{json.Number("0")},
			},
			"threads": json.Number("32"),
		},
	})
}

func (cs *clientSuite) TestEnsureQuotaGroupError(c *check.C) {
	cs.status = 500
	cs.rsp = `{"type": "error"}`
	_, err := cs.cli.EnsureQuota("foo", "bar", []string{"snap-a"}, &client.QuotaValues{Memory: quantity.Size(1)})
	c.Check(err, check.ErrorMatches, `server error: "Internal Server Error"`)
}

func (cs *clientSuite) TestGetQuotaGroupInvalidName(c *check.C) {
	_, err := cs.cli.GetQuotaGroup("")
	c.Assert(err, check.ErrorMatches, `cannot get quota group without a name`)
}

func (cs *clientSuite) TestGetQuotaGroup(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": {
			"group-name":"foo",
			"parent":"bar",
			"subgroups":["foo-subgrp"],
			"snaps":["snap-a"],
			"constraints": { "memory": 999 },
			"current": { "memory": 450 }
		}
	}`

	grp, err := cs.cli.GetQuotaGroup("foo")
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/quotas/foo")
	c.Check(grp, check.DeepEquals, &client.QuotaGroupResult{
		GroupName:   "foo",
		Parent:      "bar",
		Subgroups:   []string{"foo-subgrp"},
		Constraints: &client.QuotaValues{Memory: quantity.Size(999)},
		Current:     &client.QuotaValues{Memory: quantity.Size(450)},
		Snaps:       []string{"snap-a"},
	})
}

func (cs *clientSuite) TestGetQuotaGroupError(c *check.C) {
	cs.status = 500
	cs.rsp = `{"type": "error"}`
	_, err := cs.cli.GetQuotaGroup("foo")
	c.Check(err, check.ErrorMatches, `server error: "Internal Server Error"`)
}

func (cs *clientSuite) TestRemoveQuotaGroup(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
		"status-code": 202,
		"change": "42"
	}`

	chgID, err := cs.cli.RemoveQuotaGroup("foo")
	c.Assert(err, check.IsNil)
	c.Assert(chgID, check.Equals, "42")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/quotas")
	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action":     "remove",
		"group-name": "foo",
	})
}

func (cs *clientSuite) TestRemoveQuotaGroupError(c *check.C) {
	cs.status = 500
	cs.rsp = `{"type": "error"}`
	_, err := cs.cli.RemoveQuotaGroup("foo")
	c.Check(err, check.ErrorMatches, `cannot remove quota group: server error: "Internal Server Error"`)
}
