// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"io"
	"net/url"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
)

var errorResponseJSON = `{
	"type": "error",
	"result": {"message": "failed"}
}`

func (cs *clientSuite) TestListValidationsSetsNone(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": []
	}`

	vsets, err := cs.cli.ListValidationsSets()
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets")
	c.Check(vsets, check.HasLen, 0)
}

func (cs *clientSuite) TestListValidationsSetsError(c *check.C) {
	cs.status = 500
	cs.rsp = errorResponseJSON

	_, err := cs.cli.ListValidationsSets()
	c.Assert(err, check.ErrorMatches, "cannot list validation sets: failed")
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets")
}

func (cs *clientSuite) TestListValidationsSets(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": [
			{"account-id": "abc", "name": "def", "mode": "monitor", "sequence": 0},
			{"account-id": "ghi", "name": "jkl", "mode": "enforce", "sequence": 2}
		]
	}`

	vsets, err := cs.cli.ListValidationsSets()
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets")
	c.Check(vsets, check.DeepEquals, []*client.ValidationSetResult{
		{AccountID: "abc", Name: "def", Mode: "monitor", Sequence: 0, Valid: false},
		{AccountID: "ghi", Name: "jkl", Mode: "enforce", Sequence: 2, Valid: false},
	})
}

func (cs *clientSuite) TestApplyValidationSetMonitor(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": {"account-id": "foo", "name": "bar", "mode": "monitor", "sequence": 3, "valid": true}
	}`
	opts := &client.ValidateApplyOptions{Mode: "monitor", Sequence: 3}
	vs, err := cs.cli.ApplyValidationSet("foo", "bar", opts)
	c.Assert(err, check.IsNil)
	c.Check(vs, check.DeepEquals, &client.ValidationSetResult{
		AccountID: "foo",
		Name:      "bar",
		Mode:      "monitor",
		Sequence:  3,
		Valid:     true,
	})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets/foo/bar")
	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action":   "apply",
		"mode":     "monitor",
		"sequence": float64(3),
	})
}

func (cs *clientSuite) TestApplyValidationSetEnforce(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
        "result": {"account-id": "foo", "name": "bar", "mode": "enforce", "sequence": 3, "valid": true}
	}`
	opts := &client.ValidateApplyOptions{Mode: "enforce", Sequence: 3}
	vs, err := cs.cli.ApplyValidationSet("foo", "bar", opts)
	c.Assert(err, check.IsNil)
	c.Check(vs, check.DeepEquals, &client.ValidationSetResult{
		AccountID: "foo",
		Name:      "bar",
		Mode:      "enforce",
		Sequence:  3,
		Valid:     true,
	})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets/foo/bar")
	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action":   "apply",
		"mode":     "enforce",
		"sequence": float64(3),
	})
}

func (cs *clientSuite) TestApplyValidationSetError(c *check.C) {
	cs.status = 500
	cs.rsp = errorResponseJSON
	opts := &client.ValidateApplyOptions{Mode: "monitor"}
	_, err := cs.cli.ApplyValidationSet("foo", "bar", opts)
	c.Assert(err, check.ErrorMatches, "cannot apply validation set: failed")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets/foo/bar")
}

func (cs *clientSuite) TestApplyValidationSetInvalidArgs(c *check.C) {
	opts := &client.ValidateApplyOptions{}
	_, err := cs.cli.ApplyValidationSet("", "bar", opts)
	c.Assert(err, check.ErrorMatches, `cannot apply validation set without account ID and name`)
	_, err = cs.cli.ApplyValidationSet("", "bar", opts)
	c.Assert(err, check.ErrorMatches, `cannot apply validation set without account ID and name`)
}

func (cs *clientSuite) TestForgetValidationSet(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200
	}`
	c.Assert(cs.cli.ForgetValidationSet("foo", "bar", 3), check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets/foo/bar")
	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var req map[string]interface{}
	err = json.Unmarshal(body, &req)
	c.Assert(err, check.IsNil)
	c.Assert(req, check.DeepEquals, map[string]interface{}{
		"action":   "forget",
		"sequence": float64(3),
	})
}

func (cs *clientSuite) TestForgetValidationSetError(c *check.C) {
	cs.status = 500
	cs.rsp = errorResponseJSON
	err := cs.cli.ForgetValidationSet("foo", "bar", 0)
	c.Assert(err, check.ErrorMatches, "cannot forget validation set: failed")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets/foo/bar")
}

func (cs *clientSuite) TestForgetValidationSetInvalidArgs(c *check.C) {
	err := cs.cli.ForgetValidationSet("", "bar", 0)
	c.Assert(err, check.ErrorMatches, `cannot forget validation set without account ID and name`)
	err = cs.cli.ForgetValidationSet("", "bar", 0)
	c.Assert(err, check.ErrorMatches, `cannot forget validation set without account ID and name`)
}

func (cs *clientSuite) TestValidationSet(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": {"account-id": "abc", "name": "def", "mode": "monitor", "sequence": 0}
	}`

	vsets, err := cs.cli.ValidationSet("foo", "bar", 0)
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets/foo/bar")
	c.Check(vsets, check.DeepEquals, &client.ValidationSetResult{
		AccountID: "abc", Name: "def", Mode: "monitor", Sequence: 0, Valid: false,
	})
}

func (cs *clientSuite) TestValidationSetError(c *check.C) {
	cs.status = 500
	cs.rsp = errorResponseJSON

	_, err := cs.cli.ValidationSet("foo", "bar", 0)
	c.Assert(err, check.ErrorMatches, "cannot query validation set: failed")
}

func (cs *clientSuite) TestValidationSetInvalidArgs(c *check.C) {
	_, err := cs.cli.ValidationSet("foo", "", 0)
	c.Assert(err, check.ErrorMatches, `cannot query validation set without account ID and name`)
	_, err = cs.cli.ValidationSet("", "bar", 0)
	c.Assert(err, check.ErrorMatches, `cannot query validation set without account ID and name`)
}

func (cs *clientSuite) TestValidationSetWithSequence(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"result": {"account-id": "abc", "name": "def", "mode": "monitor", "sequence": 9}
	}`

	vsets, err := cs.cli.ValidationSet("foo", "bar", 9)
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/validation-sets/foo/bar")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{"sequence": []string{"9"}})
	c.Check(vsets, check.DeepEquals, &client.ValidationSetResult{
		AccountID: "abc", Name: "def", Mode: "monitor", Sequence: 9, Valid: false,
	})
}
