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
	"io"
	"strings"

	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

func (cs *clientSuite) TestConfdbGet(c *C) {
	cs.status = 202
	cs.rsp = `{
		"change": "123",
		"status-code": 202,
		"type": "async"
	}`

	chgID, err := cs.cli.ConfdbGetViaView("a/b/c", []string{"foo", "bar"}, map[string]string{"field": "value", "other-field": "baz"})
	c.Assert(err, IsNil)
	c.Assert(chgID, Equals, "123")
	c.Check(cs.reqs[0].Method, Equals, "GET")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/confdb/a/b/c")
	c.Check(cs.reqs[0].URL.Query().Get("keys"), DeepEquals, "foo,bar")
	constraints := cs.reqs[0].URL.Query().Get("constraints")
	cstrs := strings.Split(constraints, ",")
	c.Check(cstrs, testutil.DeepUnsortedMatches, []string{"field=value", "other-field=baz"})
}

func (cs *clientSuite) TestConfdbSet(c *C) {
	cs.status = 202
	cs.rsp = `{"type": "async", "status-code": 202, "change": "123"}`

	chgID, err := cs.cli.ConfdbSetViaView("a/b/c", map[string]any{"foo": "bar", "baz": json.Number("1")})
	c.Check(err, IsNil)
	c.Check(chgID, Equals, "123")
	c.Assert(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "PUT")
	c.Check(cs.reqs[0].Header.Get("Content-Type"), Equals, "application/json")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/confdb/a/b/c")
	data, err := io.ReadAll(cs.reqs[0].Body)
	c.Assert(err, IsNil)

	// need to decode because entries may have been encoded in any order
	res := make(map[string]any)
	err = json.Unmarshal(data, &res)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, map[string]any{"foo": "bar", "baz": float64(1)})
}
