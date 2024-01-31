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
	"fmt"
	"io/ioutil"

	. "gopkg.in/check.v1"
)

func (cs *clientSuite) testClientReseal(c *C, reboot bool) {
	cs.status = 202
	cs.rsp = `{"type":"async", "status-code": 202, "change": "42"}`

	chg, err := cs.cli.Reseal(reboot)
	c.Assert(err, IsNil)
	c.Check(chg, Equals, "42")

	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "POST")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/system-reseal")
	data, err := ioutil.ReadAll(cs.reqs[0].Body)
	c.Assert(err, IsNil)
	c.Check(string(data), Equals, fmt.Sprintf(`{"reboot":%v}`, reboot))
}

func (cs *clientSuite) TestClientResealRebootHappy(c *C) {
	cs.testClientReseal(c, true)
}
func (cs *clientSuite) TestClientResealNoRebootHappy(c *C) {
	cs.testClientReseal(c, false)
}

func (cs *clientSuite) TestClientResealError(c *C) {
	cs.status = 500
	cs.rsp = `{"type": "error", "status-code": 500, "result":{"message":"boom","kind":"err-kind","value":"err-value"}}`

	_, err := cs.cli.Reseal(true)
	c.Check(err, ErrorMatches, "boom")
	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "POST")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/system-reseal")
}
