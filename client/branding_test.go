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
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

func (cs *clientSuite) TestClientBranding(c *C) {
	cs.rsp = `{
		"result": {
			"name": "amd64",
			"subname": "generic"
		},
		"type": "sync"
	}`
	branding, err := cs.cli.Branding()
	c.Assert(err, IsNil)
	c.Assert(cs.req.Method, Equals, "GET")
	c.Assert(cs.req.URL.Path, Equals, "/2.0/branding")
	c.Assert(branding, DeepEquals, &client.Branding{
		Name:    "amd64",
		SubName: "generic",
	})
}
