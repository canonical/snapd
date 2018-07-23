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
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

func (cs *clientSuite) TestClientAssert(c *C) {
	cs.rsp = `{
		"type": "sync",
		"result": {}
	}`
	a := []byte("Assertion.")
	err := cs.cli.Ack(a)
	c.Assert(err, IsNil)
	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	c.Check(body, DeepEquals, a)
	c.Check(cs.req.Method, Equals, "POST")
	c.Check(cs.req.URL.Path, Equals, "/v2/assertions")
}

func (cs *clientSuite) TestClientAssertsTypes(c *C) {
	cs.rsp = `{
    "result": {
        "types": ["one", "two"]
    },
    "status": "OK",
    "status-code": 200,
    "type": "sync"
}`
	typs, err := cs.cli.AssertionTypes()
	c.Assert(err, IsNil)
	c.Check(typs, DeepEquals, []string{"one", "two"})
}

func (cs *clientSuite) TestClientAssertsCallsEndpoint(c *C) {
	_, _ = cs.cli.Known("snap-revision", nil)
	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.URL.Path, Equals, "/v2/assertions/snap-revision")
}

func (cs *clientSuite) TestClientAssertsCallsEndpointWithFilter(c *C) {
	_, _ = cs.cli.Known("snap-revision", map[string]string{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": "sha3-384...",
	})
	u, err := url.ParseRequestURI(cs.req.URL.String())
	c.Assert(err, IsNil)
	c.Check(u.Path, Equals, "/v2/assertions/snap-revision")
	c.Check(u.Query(), DeepEquals, url.Values{
		"snap-sha3-384": []string{"sha3-384..."},
		"snap-id":       []string{"snap-id-1"},
	})
}

func (cs *clientSuite) TestClientAssertsHttpError(c *C) {
	cs.err = errors.New("fail")
	_, err := cs.cli.Known("snap-build", nil)
	c.Assert(err, ErrorMatches, "failed to query assertions: cannot communicate with server: fail")
}

func (cs *clientSuite) TestClientAssertsJSONError(c *C) {
	cs.status = 400
	cs.header = http.Header{}
	cs.header.Add("Content-type", "application/json")
	cs.rsp = `{
		"status-code": 400,
		"type": "error",
		"result": {
			"message": "invalid"
		}
	}`
	_, err := cs.cli.Known("snap-build", nil)
	c.Assert(err, ErrorMatches, "invalid")
}

func (cs *clientSuite) TestClientAsserts(c *C) {
	cs.header = http.Header{}
	cs.header.Add("X-Ubuntu-Assertions-Count", "2")
	cs.rsp = `type: snap-revision
authority-id: store-id1
snap-sha3-384: P1wNUk5O_5tO5spqOLlqUuAk7gkNYezIMHp5N9hMUg1a6YEjNeaCc4T0BaYz7IWs
snap-id: snap-id-1
snap-size: 123
snap-revision: 1
developer-id: dev-id1
revision: 1
timestamp: 2015-11-25T20:00:00Z
body-length: 0
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

openpgp ...

type: snap-revision
authority-id: store-id1
snap-sha3-384: 0Yt6-GXQeTZWUAHo1IKDpS9kqO6zMaizY6vGEfGM-aSfpghPKir1Ic7teQ5Zadaj
snap-id: snap-id-2
snap-size: 456
snap-revision: 1
developer-id: dev-id1
revision: 1
timestamp: 2015-11-30T20:00:00Z
body-length: 0
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

openpgp ...
`

	a, err := cs.cli.Known("snap-revision", nil)
	c.Assert(err, IsNil)
	c.Check(a, HasLen, 2)

	c.Check(a[0].Type(), Equals, asserts.SnapRevisionType)
}

func (cs *clientSuite) TestClientAssertsNoAssertions(c *C) {
	cs.header = http.Header{}
	cs.header.Add("X-Ubuntu-Assertions-Count", "0")
	cs.rsp = ""
	cs.status = 200
	a, err := cs.cli.Known("snap-revision", nil)
	c.Assert(err, IsNil)
	c.Check(a, HasLen, 0)
}

func (cs *clientSuite) TestClientAssertsMissingAssertions(c *C) {
	cs.header = http.Header{}
	cs.header.Add("X-Ubuntu-Assertions-Count", "4")
	cs.rsp = ""
	cs.status = 200
	_, err := cs.cli.Known("snap-build", nil)
	c.Assert(err, ErrorMatches, "response did not have the expected number of assertions")
}
