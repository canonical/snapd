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
	"errors"
	"fmt"
	"io/ioutil"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

var ops = []struct {
	op     func(*client.Client, string) (string, error)
	action string
}{
	{(*client.Client).AddSnap, "install"},
	{(*client.Client).RemoveSnap, "remove"},
	{(*client.Client).RefreshSnap, "update"},
	{(*client.Client).PurgeSnap, "purge"},
	{(*client.Client).RollbackSnap, "rollback"},
	{(*client.Client).ActivateSnap, "activate"},
	{(*client.Client).DeactivateSnap, "deactivate"},
}

func (cs *clientSuite) TestClientOpSnapServerError(c *check.C) {
	cs.err = errors.New("fail")
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Check(err, check.ErrorMatches, `.*fail`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapResponseError(c *check.C) {
	cs.rsp = `{"type": "error", "status": "potatoes"}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Check(err, check.ErrorMatches, `.*server error: "potatoes"`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapBadType(c *check.C) {
	cs.rsp = `{"type": "what"}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Check(err, check.ErrorMatches, `.*expected async response for "POST" on "/2.0/snaps/`+pkgName+`", got "what"`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapNotAccepted(c *check.C) {
	cs.rsp = `{
		"status_code": 200,
		"type": "async"
	}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Check(err, check.ErrorMatches, `.*operation not accepted`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapInvalidResult(c *check.C) {
	cs.rsp = `{
		"result": "not a JSON object",
		"status_code": 202,
		"type": "async"
	}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Assert(err, check.ErrorMatches, `.*cannot unmarshal result.*`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapNoResource(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status_code": 202,
		"type": "async"
	}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Assert(err, check.ErrorMatches, `.*invalid resource location.*`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapInvalidResource(c *check.C) {
	cs.rsp = `{
		"result": {
			"resource": "invalid"
		},
		"status_code": 202,
		"type": "async"
	}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Assert(err, check.ErrorMatches, `.*invalid resource location.*`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnap(c *check.C) {
	cs.rsp = `{
		"result": {
			"resource": "/2.0/operations/5a70dffa-66b3-3567-d728-55b0da48bdc7"
		},
		"status_code": 202,
		"type": "async"
	}`
	for _, s := range ops {
		uuid, err := s.op(cs.cli, pkgName)

		body, err := ioutil.ReadAll(cs.req.Body)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		jsonBody := make(map[string]string)
		err = json.Unmarshal(body, &jsonBody)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		c.Check(jsonBody["action"], check.Equals, s.action, check.Commentf(s.action))

		c.Check(cs.req.Method, check.Equals, "POST", check.Commentf(s.action))
		c.Check(cs.req.URL.Path, check.Equals, fmt.Sprintf("/2.0/snaps/%s", pkgName), check.Commentf(s.action))
		c.Check(uuid, check.Equals, "5a70dffa-66b3-3567-d728-55b0da48bdc7", check.Commentf(s.action))
	}
}
