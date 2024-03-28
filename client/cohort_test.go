// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"io"

	"golang.org/x/xerrors"
	"gopkg.in/check.v1"
)

func (cs *clientSuite) TestClientCreateCohortsEndpoint(c *check.C) {
	cs.cli.CreateCohorts([]string{"foo", "bar"})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/cohorts")

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var jsonBody map[string]interface{}
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, check.IsNil)
	c.Check(jsonBody, check.DeepEquals, map[string]interface{}{
		"action": "create",
		"snaps":  []interface{}{"foo", "bar"},
	})
}

func (cs *clientSuite) TestClientCreateCohorts(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
                "result": {"foo": "xyzzy", "bar": "what-what"}
	}`
	cohorts, err := cs.cli.CreateCohorts([]string{"foo", "bar"})
	c.Assert(err, check.IsNil)
	c.Check(cohorts, check.DeepEquals, map[string]string{
		"foo": "xyzzy",
		"bar": "what-what",
	})

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	var jsonBody map[string]interface{}
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, check.IsNil)
	c.Check(jsonBody, check.DeepEquals, map[string]interface{}{
		"action": "create",
		"snaps":  []interface{}{"foo", "bar"},
	})
}

func (cs *clientSuite) TestClientCreateCohortsErrIsWrapped(c *check.C) {
	cs.err = errors.New("boom")
	_, err := cs.cli.CreateCohorts([]string{"foo", "bar"})
	var e xerrors.Wrapper
	c.Assert(err, check.Implements, &e)
}
