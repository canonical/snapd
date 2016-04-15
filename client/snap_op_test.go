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
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

var chanName = "achan"

var ops = []struct {
	op     func(*client.Client, string) (string, error)
	action string
}{
	{(*client.Client).RemoveSnap, "remove"},
	{(*client.Client).PurgeSnap, "purge"},
	{(*client.Client).RollbackSnap, "rollback"},
	{(*client.Client).ActivateSnap, "activate"},
	{(*client.Client).DeactivateSnap, "deactivate"},
}

var chanops = []struct {
	op     func(*client.Client, string, string) (string, error)
	action string
}{
	{(*client.Client).InstallSnap, "install"},
	{(*client.Client).RefreshSnap, "refresh"},
}

func (cs *clientSuite) TestClientOpSnapServerError(c *check.C) {
	cs.err = errors.New("fail")
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Check(err, check.ErrorMatches, `.*fail`, check.Commentf(s.action))
	}

	for _, s := range chanops {
		_, err := s.op(cs.cli, pkgName, chanName)
		c.Check(err, check.ErrorMatches, `.*fail`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapResponseError(c *check.C) {
	cs.rsp = `{"type": "error", "status": "potatoes"}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Check(err, check.ErrorMatches, `.*server error: "potatoes"`, check.Commentf(s.action))
	}

	for _, s := range chanops {
		_, err := s.op(cs.cli, pkgName, chanName)
		c.Check(err, check.ErrorMatches, `.*server error: "potatoes"`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapBadType(c *check.C) {
	cs.rsp = `{"type": "what"}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Check(err, check.ErrorMatches, `.*expected async response for "POST" on "/v2/snaps/`+pkgName+`", got "what"`, check.Commentf(s.action))
	}

	for _, s := range chanops {
		_, err := s.op(cs.cli, pkgName, chanName)
		c.Check(err, check.ErrorMatches, `.*expected async response for "POST" on "/v2/snaps/`+pkgName+`", got "what"`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapNotAccepted(c *check.C) {
	cs.rsp = `{
		"status-code": 200,
		"type": "async"
	}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Check(err, check.ErrorMatches, `.*operation not accepted`, check.Commentf(s.action))
	}

	for _, s := range chanops {
		_, err := s.op(cs.cli, pkgName, chanName)
		c.Check(err, check.ErrorMatches, `.*operation not accepted`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnapNoChange(c *check.C) {
	cs.rsp = `{
		"status-code": 202,
		"type": "async"
	}`
	for _, s := range ops {
		_, err := s.op(cs.cli, pkgName)
		c.Assert(err, check.ErrorMatches, `.*response without change reference.*`, check.Commentf(s.action))
	}

	for _, s := range chanops {
		_, err := s.op(cs.cli, pkgName, chanName)
		c.Assert(err, check.ErrorMatches, `.*response without change reference.*`, check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSnap(c *check.C) {
	cs.rsp = `{
		"change": "d728",
		"status-code": 202,
		"type": "async"
	}`
	for _, s := range ops {
		id, err := s.op(cs.cli, pkgName)
		c.Assert(err, check.IsNil)

		body, err := ioutil.ReadAll(cs.req.Body)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		jsonBody := make(map[string]string)
		err = json.Unmarshal(body, &jsonBody)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		c.Check(jsonBody["action"], check.Equals, s.action, check.Commentf(s.action))
		c.Check(jsonBody, check.HasLen, 1, check.Commentf(s.action))

		c.Check(cs.req.Method, check.Equals, "POST", check.Commentf(s.action))
		c.Check(cs.req.URL.Path, check.Equals, fmt.Sprintf("/v2/snaps/%s", pkgName), check.Commentf(s.action))
		c.Check(id, check.Equals, "d728", check.Commentf(s.action))
	}

	for _, s := range chanops {
		id, err := s.op(cs.cli, pkgName, chanName)
		c.Assert(err, check.IsNil)

		body, err := ioutil.ReadAll(cs.req.Body)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		jsonBody := make(map[string]string)
		err = json.Unmarshal(body, &jsonBody)
		c.Assert(err, check.IsNil, check.Commentf(s.action))
		c.Check(jsonBody["action"], check.Equals, s.action, check.Commentf(s.action))
		c.Check(jsonBody["channel"], check.Equals, chanName, check.Commentf(s.action))
		c.Check(jsonBody, check.HasLen, 2, check.Commentf(s.action))

		c.Check(cs.req.Method, check.Equals, "POST", check.Commentf(s.action))
		c.Check(cs.req.URL.Path, check.Equals, fmt.Sprintf("/v2/snaps/%s", pkgName), check.Commentf(s.action))
		c.Check(id, check.Equals, "d728", check.Commentf(s.action))
	}
}

func (cs *clientSuite) TestClientOpSideload(c *check.C) {
	cs.rsp = `{
		"change": "66b3",
		"status-code": 202,
		"type": "async"
	}`
	bodyData := []byte("snap-data")

	snap := filepath.Join(c.MkDir(), "foo.snap")
	err := ioutil.WriteFile(snap, bodyData, 0644)
	c.Assert(err, check.IsNil)

	id, err := (*client.Client).InstallSnapPath(cs.cli, snap)
	c.Assert(err, check.IsNil)

	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	c.Assert(body, check.DeepEquals, bodyData)

	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, fmt.Sprintf("/v2/snaps"))
	c.Check(id, check.Equals, "66b3")
}
