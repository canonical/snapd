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
	"gopkg.in/check.v1"
)

func (cs *clientSuite) TestClientOpRunning(c *check.C) {
	cs.rsp = `{
"type": "sync", "result": {
  "resource": "/v2/operations/foo",
  "status":   "running",
  "created-at": "2010-01-01T01:01:01.010101Z",
  "updated-at": "2016-01-01T01:01:01.010101Z",
  "may-cancel": false,
  "output": {}
}}`
	op, err := cs.cli.Operation("foo")
	c.Assert(err, check.IsNil)
	c.Check(op.Running(), check.Equals, true)
	c.Check(op.Err(), check.IsNil)
}

func (cs *clientSuite) TestClientOpFailed(c *check.C) {
	cs.rsp = `{
"type": "sync", "result": {
  "resource": "/v2/operations/foo",
  "status":   "failed",
  "created-at": "2010-01-01T01:01:01.010101Z",
  "updated-at": "2016-01-01T01:01:01.010101Z",
  "may-cancel": false,
  "output": {"message": "something broke"}
}}`
	op, err := cs.cli.Operation("foo")
	c.Assert(err, check.IsNil)
	c.Check(op.Running(), check.Equals, false)
	c.Check(op.Err(), check.ErrorMatches, "something broke")
}

func (cs *clientSuite) TestClientOpFailedFailure(c *check.C) {
	cs.rsp = `{
"type": "sync", "result": {
  "resource": "/v2/operations/foo",
  "status":   "failed",
  "created-at": "2010-01-01T01:01:01.010101Z",
  "updated-at": "2016-01-01T01:01:01.010101Z",
  "may-cancel": false,
  "output": false
}}`
	op, err := cs.cli.Operation("foo")
	c.Assert(err, check.IsNil)
	c.Check(op.Running(), check.Equals, false)
	c.Check(op.Err(), check.ErrorMatches, `unexpected error format: "false"`)
}

func (cs *clientSuite) TestClientOpSucceeded(c *check.C) {
	cs.rsp = `{
"type": "sync", "result": {
  "resource": "/v2/operations/foo",
  "status":   "succeeded",
  "created-at": "2010-01-01T01:01:01.010101Z",
  "updated-at": "2016-01-01T01:01:01.010101Z",
  "may-cancel": false,
  "output": {}
}}`
	op, err := cs.cli.Operation("foo")
	c.Assert(err, check.IsNil)
	c.Check(op.Running(), check.Equals, false)
	c.Check(op.Err(), check.IsNil)
}
