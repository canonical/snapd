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
	"github.com/ddkwork/golibrary/mylog"
	. "gopkg.in/check.v1"
)

func (cs *clientSuite) TestClientInternalConsoleConfEndpointEmpty(c *C) {
	// no changes and no snaps
	cs.status = 200
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
        "result": {}
	}`

	chgs, snaps := mylog.Check3(cs.cli.InternalConsoleConfStart())
	c.Assert(chgs, HasLen, 0)
	c.Assert(snaps, HasLen, 0)

	c.Check(cs.req.Method, Equals, "POST")
	c.Check(cs.req.URL.Path, Equals, "/v2/internal/console-conf-start")
	c.Check(cs.doCalls, Equals, 1)
}

func (cs *clientSuite) TestClientInternalConsoleConfEndpoint(c *C) {
	// some changes and snaps
	cs.status = 200
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
        "result": {
			"active-auto-refreshes": ["1"],
			"active-auto-refresh-snaps": ["pc-kernel"]
		}
	}`

	chgs, snaps := mylog.Check3(cs.cli.InternalConsoleConfStart())

	c.Assert(chgs, DeepEquals, []string{"1"})
	c.Assert(snaps, DeepEquals, []string{"pc-kernel"})
	c.Check(cs.req.Method, Equals, "POST")
	c.Check(cs.req.URL.Path, Equals, "/v2/internal/console-conf-start")
	c.Check(cs.doCalls, Equals, 1)
}
