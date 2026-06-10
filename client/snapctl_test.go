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

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/testutil"
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

	stdout, stderr, err := cs.cli.RunSnapctl(options, mockStdin)
	c.Assert(err, check.IsNil)
	c.Check(string(stdout), check.Equals, "test stdout")
	c.Check(string(stderr), check.Equals, "test stderr")

	var body map[string]any
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]any{
		"context-id": "1234ABCD",
		"args":       []any{"foo", "bar"},

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

	_, _, err := cs.cli.RunSnapctl(options, mockStdin)
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

	_, _, err := cs.cli.RunSnapctl(options, mockStdin)
	c.Check(err, check.IsNil)
}

func (cs *clientSuite) TestClientRunSnapctlHeader(c *check.C) {
	cs.rsp = `{
        "type": "sync",
        "status-code": 200,
        "result": {}
    }`

	options := &client.SnapCtlOptions{
		ContextID: "1234ABCD",
		Args:      []string{"foo", "bar"},
	}
	_, _, err := cs.cli.RunSnapctl(options, nil)

	for _, feature := range client.TestSupportedFeatures {
		c.Check(cs.req.Header.Get("X-Snapctl-Features"), testutil.Contains, feature)
	}
	c.Check(err, check.IsNil)
}

func (cs *clientSuite) TestClientRunSnapctlAsync(c *check.C) {
	cs.rsps = []string{
		`{
			"type": "sync",
			"status-code": 200,
			"result": {
				"change-id": "123"
			}
		}`,
		`{
			"type": "error",
			"status-code": 400,
			"result": {
				"message": "snap is not ready",
				"kind": "unsuccessful",
				"value": {
					"exit-code": 3
				}
			}
		}`,
		`{
			"type": "sync",
			"status-code": 200,
			"result": {
				"stdout": "snap installed",
				"stderr": ""
			}
		}`,
	}

	options := &client.SnapCtlOptions{
		ContextID: "1234ABCD",
		Args:      []string{"install", "some-snap"},
	}
	stdout, stderr, err := cs.cli.RunSnapctl(options, nil)

	c.Assert(err, check.IsNil)
	c.Check(string(stdout), check.Equals, "snap installed")
	c.Check(stderr, check.HasLen, 0)

	c.Assert(cs.reqs, check.HasLen, 3)

	// Check the daemon makes the is-ready call, and loops when error code 1 is returned
	var payload0 map[string]any
	err = json.NewDecoder(cs.reqs[0].Body).Decode(&payload0)
	c.Check(err, check.IsNil)
	c.Check(payload0["args"], check.DeepEquals, []any{"install", "some-snap"})

	var payload1 map[string]any
	err = json.NewDecoder(cs.reqs[1].Body).Decode(&payload1)
	c.Check(err, check.IsNil)
	c.Check(payload1["args"], check.DeepEquals, []any{"is-ready", "123"})

	var payload2 map[string]any
	err = json.NewDecoder(cs.reqs[2].Body).Decode(&payload2)
	c.Check(err, check.IsNil)
	c.Check(payload2["args"], check.DeepEquals, []any{"is-ready", "123"})
}

func (cs *clientSuite) TestClientRunSnapctlAsyncFatalError(c *check.C) {
	cs.rsps = []string{
		`{
			"type": "sync",
			"status-code": 200,
			"result": {
				"change-id": "123"
			}
		}`,
		`{
			"type": "error",
			"status-code": 400,
			"result": {
				"message": "snap is not ready",
				"kind": "unsuccessful",
				"value": {
					"exit-code": 2
				}
			}
		}`,
	}

	options := &client.SnapCtlOptions{
		ContextID: "1234ABCD",
		Args:      []string{"install", "some-snap"},
	}
	stdout, stderr, err := cs.cli.RunSnapctl(options, nil)

	c.Assert(err, check.NotNil)
	c.Check(string(stdout), check.Equals, "")
	c.Check(stderr, check.HasLen, 0)

	c.Assert(cs.reqs, check.HasLen, 2)

	// Check the daemon makes the is-ready call, and stops looping when non 1
	// error code is returned
	var payload0 map[string]any
	err = json.NewDecoder(cs.reqs[0].Body).Decode(&payload0)
	c.Check(err, check.IsNil)
	c.Check(payload0["args"], check.DeepEquals, []any{"install", "some-snap"})

	var payload1 map[string]any
	err = json.NewDecoder(cs.reqs[1].Body).Decode(&payload1)
	c.Check(err, check.IsNil)
	c.Check(payload1["args"], check.DeepEquals, []any{"is-ready", "123"})
}
