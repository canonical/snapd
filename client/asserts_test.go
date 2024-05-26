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
	"io"
	"net/http"
	"net/url"

	"golang.org/x/xerrors"
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/snap"
)

func (cs *clientSuite) TestClientAssert(c *C) {
	cs.rsp = `{
		"type": "sync",
		"result": {}
	}`
	a := []byte("Assertion.")
	mylog.Check(cs.cli.Ack(a))

	body := mylog.Check2(io.ReadAll(cs.req.Body))

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
	typs := mylog.Check2(cs.cli.AssertionTypes())

	c.Check(typs, DeepEquals, []string{"one", "two"})
}

func (cs *clientSuite) TestClientAssertsCallsEndpoint(c *C) {
	_, _ = cs.cli.Known("snap-revision", nil, nil)
	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.URL.Path, Equals, "/v2/assertions/snap-revision")
}

func (cs *clientSuite) TestClientAssertsOptsCallsEndpoint(c *C) {
	_, _ = cs.cli.Known("snap-revision", nil, &client.KnownOptions{Remote: true})
	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.URL.Path, Equals, "/v2/assertions/snap-revision")
	c.Check(cs.req.URL.Query()["remote"], DeepEquals, []string{"true"})
}

func (cs *clientSuite) TestClientAssertsCallsEndpointWithFilter(c *C) {
	_, _ = cs.cli.Known("snap-revision", map[string]string{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": "sha3-384...",
	}, nil)
	u := mylog.Check2(url.ParseRequestURI(cs.req.URL.String()))

	c.Check(u.Path, Equals, "/v2/assertions/snap-revision")
	c.Check(u.Query(), DeepEquals, url.Values{
		"snap-sha3-384": []string{"sha3-384..."},
		"snap-id":       []string{"snap-id-1"},
	})
}

func (cs *clientSuite) TestClientAssertsHttpError(c *C) {
	cs.err = errors.New("fail")
	_ := mylog.Check2(cs.cli.Known("snap-build", nil, nil))
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
	_ := mylog.Check2(cs.cli.Known("snap-build", nil, nil))
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

	a := mylog.Check2(cs.cli.Known("snap-revision", nil, nil))

	c.Check(a, HasLen, 2)

	c.Check(a[0].Type(), Equals, asserts.SnapRevisionType)
}

func (cs *clientSuite) TestClientAssertsNoAssertions(c *C) {
	cs.header = http.Header{}
	cs.header.Add("X-Ubuntu-Assertions-Count", "0")
	cs.rsp = ""
	cs.status = 200
	a := mylog.Check2(cs.cli.Known("snap-revision", nil, nil))

	c.Check(a, HasLen, 0)
}

func (cs *clientSuite) TestClientAssertsMissingAssertions(c *C) {
	cs.header = http.Header{}
	cs.header.Add("X-Ubuntu-Assertions-Count", "4")
	cs.rsp = ""
	cs.status = 200
	_ := mylog.Check2(cs.cli.Known("snap-build", nil, nil))
	c.Assert(err, ErrorMatches, "response did not have the expected number of assertions")
}

func (cs *clientSuite) TestStoreAccount(c *C) {
	cs.header = http.Header{}
	cs.header.Add("X-Ubuntu-Assertions-Count", "1")
	cs.rsp = `type: account
authority-id: canonical
account-id: canonicalID
display-name: canonicalDisplay
timestamp: 2016-04-01T00:00:00.0Z
username: canonicalUser
validation: certified
sign-key-sha3-384: -CvQKAwRQ5h3Ffn10FILJoEZUXOv6km9FwA80-Rcj-f-6jadQ89VRswHNiEB9Lxk

AcLDXAQAAQoABgUCV7UYzwAKCRDUpVvql9g3IK7uH/4udqNOurx5WYVknzXdwekp0ovHCQJ0iBPw
TSFxEVr9faZSzb7eqJ1WicHsShf97PYS3ClRYAiluFsjRA8Y03kkSVJHjC+sIwGFubsnkmgflt6D
WEmYIl0UBmeaEDS8uY4Xvp9NsLTzNEj2kvzy/52gKaTc1ZSl5RDL9ppMav+0V9iBYpiDPBWH2rJ+
aDSD8Rkyygm0UscfAKyDKH4lrvZ0WkYyi1YVNPrjQ/AtBySh6Q4iJ3LifzKa9woIyAuJET/4/FPY
oirqHAfuvNod36yNQIyNqEc20AvTvZNH0PSsg4rq3DLjIPzv5KbJO9lhsasNJK1OdL6x8Yqrdsbk
ldZp4qkzfjV7VOMQKaadfcZPRaVVeJWOBnBiaukzkhoNlQi1sdCdkBB/AJHZF8QXw6c7vPDcfnCV
1lW7ddQ2p8IsJbT6LzpJu3GW/P4xhNgCjtCJ1AJm9a9RqLwQYgdLZwwDa9iCRtqTbRXBlfy3apps
1VjbQ3h5iCd0hNfwDBnGVm1rhLKHCD1DUdNE43oN2ZlE7XGyh0HFV6vKlpqoW3eoXCIxWu+HBY96
+LSl/jQgCkb0nxYyzEYK4Reb31D0mYw1Nji5W+MIF5E09+DYZoOT0UvR05YMwMEOeSdI/hLWg/5P
k+GDK+/KopMmpd4D1+jjtF7ZvqDpmAV98jJGB2F88RyVb4gcjmFFyTi4Kv6vzz/oLpbm0qrizC0W
HLGDN/ymGA5sHzEgEx7U540vz/q9VX60FKqL2YZr/DcyY9GKX5kCG4sNqIIHbcJneZ4frM99oVDu
7Jv+DIx/Di6D1ULXol2XjxbbJLKHFtHksR97ceaFvcZwTogC61IYUBJCvvMoqdXAWMhEXCr0QfQ5
Xbi31XW2d4/lF/zWlAkRnGTzufIXFni7+nEuOK0SQEzO3/WaRedK1SGOOtTDjB8/3OJeW96AUYK5
oTIynkYkEyHWMNCXALg+WQW6L4/YO7aUjZ97zOWIugd7Xy63aT3r/EHafqaY2nacOhLfkeKZ830b
o/ezjoZQAxbh6ce7JnXRgE9ELxjdAhBTpGjmmmN2sYrJ7zP9bOgly0BnEPXGSQfFA+NNNw1FADx1
MUY8q9DBjmVtgqY+1KGTV5X8KvQCBMODZIf/XJPHdCRAHxMd8COypcwgL2vDIIXpOFbi1J/B0GF+
eklxk9wzBA8AecBMCwCzIRHDNpD1oa2we38bVFrOug6e/VId1k1jYFJjiLyLCDmV8IMYwEllHSXp
LQAdm3xZ7t4WnxYC8YSCk9mXf3CZg59SpmnV5Q5Z6A5Pl7Nc3sj7hcsMBZEsOMPzNC9dPsBnZvjs
WpPUffJzEdhHBFhvYMuD4Vqj6ejUv9l3oTrjQWVC
`

	account := mylog.Check2(cs.cli.StoreAccount("canonicalID"))

	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.URL.Query(), HasLen, 1)
	c.Check(cs.req.URL.Query().Get("account-id"), Equals, "canonicalID")
	c.Assert(account, DeepEquals, &snap.StoreAccount{
		ID:          "canonicalID",
		Username:    "canonicalUser",
		DisplayName: "canonicalDisplay",
		Validation:  "verified",
	})
}

func (cs *clientSuite) TestStoreAccountNoAssertionFound(c *C) {
	cs.header = http.Header{}
	cs.header.Add("X-Ubuntu-Assertions-Count", "0")
	cs.rsp = ""

	_ := mylog.Check2(cs.cli.StoreAccount("canonicalID"))
	c.Assert(err, ErrorMatches, "no assertion found for account-id canonicalID")
}

func (cs *clientSuite) TestClientAssertTypesErrIsWrapped(c *C) {
	cs.err = errors.New("boom")
	_ := mylog.Check2(cs.cli.AssertionTypes())
	var e xerrors.Wrapper
	c.Assert(err, Implements, &e)
}

func (cs *clientSuite) TestClientKnownErrIsWrapped(c *C) {
	cs.err = errors.New("boom")
	_ := mylog.Check2(cs.cli.Known("foo", nil, nil))
	var e xerrors.Wrapper
	c.Assert(err, Implements, &e)
}
