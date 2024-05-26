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
	"bytes"
	"encoding/base64"
	"encoding/json"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) TestClientRunSnapctlCallsEndpoint(c *check.C) {
	options := &client.SnapCtlOptions{
		ContextID: "1234ABCD",
		Args:      []string{"foo", "bar"},
	}
	cs.cli.RunSnapctl(options, nil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snapctl")
}

func (cs *clientSuite) TestClientRunSnapctl(c *check.C) {
	cs.rsp = `{
		"type": "sync",
        "status-code": 200,
		"result": {
			"stdout": "test stdout",
			"stderr": "test stderr"
		}
	}`

	mockStdin := bytes.NewBufferString("some-input")
	options := &client.SnapCtlOptions{
		ContextID: "1234ABCD",
		Args:      []string{"foo", "bar"},
	}

	stdout, stderr := mylog.Check3(cs.cli.RunSnapctl(options, mockStdin))
	c.Assert(err, check.IsNil)
	c.Check(string(stdout), check.Equals, "test stdout")
	c.Check(string(stderr), check.Equals, "test stderr")

	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	mylog.Check(decoder.Decode(&body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"context-id": "1234ABCD",
		"args":       []interface{}{"foo", "bar"},

		// json byte-stream is b64 encoded
		"stdin": base64.StdEncoding.EncodeToString([]byte("some-input")),
	})
}

func (cs *clientSuite) TestInternalSnapctlCmdNeedsStdin(c *check.C) {
	res := client.InternalSnapctlCmdNeedsStdin("fde-setup-result")
	c.Check(res, check.Equals, true)

	for _, s := range []string{"help", "other"} {
		res := client.InternalSnapctlCmdNeedsStdin(s)
		c.Check(res, check.Equals, false)
	}
}

func (cs *clientSuite) TestClientRunSnapctlReadLimitOneTooMuch(c *check.C) {
	cs.rsp = `{
		"type": "sync",
        "status-code": 200,
		"result": {
		}
	}`

	restore := client.MockStdinReadLimit(10)
	defer restore()

	mockStdin := bytes.NewBufferString("12345678901")
	options := &client.SnapCtlOptions{
		ContextID: "1234ABCD",
		Args:      []string{"foo", "bar"},
	}

	_, _ := mylog.Check3(cs.cli.RunSnapctl(options, mockStdin))
	c.Check(err, check.ErrorMatches, "cannot read more than 10 bytes of data from stdin")
}

func (cs *clientSuite) TestClientRunSnapctlReadLimitExact(c *check.C) {
	cs.rsp = `{
		"type": "sync",
        "status-code": 200,
		"result": {
		}
	}`

	restore := client.MockStdinReadLimit(10)
	defer restore()

	mockStdin := bytes.NewBufferString("1234567890")
	options := &client.SnapCtlOptions{
		ContextID: "1234ABCD",
		Args:      []string{"foo", "bar"},
	}

	_, _ := mylog.Check3(cs.cli.RunSnapctl(options, mockStdin))
	c.Check(err, check.IsNil)
}
