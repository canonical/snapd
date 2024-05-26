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
	"net/url"

	"github.com/ddkwork/golibrary/mylog"
	. "gopkg.in/check.v1"
)

func (cs *clientSuite) TestAspectGet(c *C) {
	cs.rsp = `{"type": "sync", "result":{"foo":"baz","bar":1}}`

	res := mylog.Check2(cs.cli.AspectGet("a/b/c", []string{"foo", "bar"}))
	c.Check(err, IsNil)
	c.Check(res, DeepEquals, map[string]interface{}{"foo": "baz", "bar": json.Number("1")})
	c.Assert(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "GET")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/aspects/a/b/c")
	c.Check(cs.reqs[0].URL.Query(), DeepEquals, url.Values{"fields": []string{"foo,bar"}})
}

func (cs *clientSuite) TestAspectSet(c *C) {
	cs.status = 202
	cs.rsp = `{"type": "async", "status-code": 202, "change": "123"}`

	chgID := mylog.Check2(cs.cli.AspectSet("a/b/c", map[string]interface{}{"foo": "bar", "baz": json.Number("1")}))
	c.Check(err, IsNil)
	c.Check(chgID, Equals, "123")
	c.Assert(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "PUT")
	c.Check(cs.reqs[0].Header.Get("Content-Type"), Equals, "application/json")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/aspects/a/b/c")
	data := mylog.Check2(io.ReadAll(cs.reqs[0].Body))


	// need to decode because entries may have been encoded in any order
	res := make(map[string]interface{})
	mylog.Check(json.Unmarshal(data, &res))

	c.Check(res, DeepEquals, map[string]interface{}{"foo": "bar", "baz": float64(1)})
}
