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
	"io/ioutil"

	. "gopkg.in/check.v1"
)

func (cs *clientSuite) TestClientAssert(c *C) {
	cs.rsp = `{
		"type": "sync",
		"result": {}
	}`
	a := []byte("Assertion.")
	err := cs.cli.Assert(a)
	c.Assert(err, IsNil)
	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	c.Check(body, DeepEquals, a)
	c.Check(cs.req.Method, Equals, "POST")
	c.Check(cs.req.URL.Path, Equals, "/2.0/assertions")
}
