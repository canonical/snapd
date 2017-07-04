// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/asserts"
	repair "github.com/snapcore/snapd/cmd/snap-repair"
)

var (
	testKey = `type: account-key
authority-id: canonical
account-id: canonical
name: repair
public-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj
since: 2015-11-16T15:04:00Z
body-length: 149
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AcZrBFaFwYABAvCX5A8dTcdLdhdiuy2YRHO5CAfM5InQefkKOhNMUq2yfi3Sk6trUHxskhZkPnm4
NKx2yRr332q7AJXQHLX+DrZ29ycyoQ2NQGO3eAfQ0hjAAQFYBF8SSh5SutPu5XCVABEBAAE=

AXNpZw==
`

	testRepair = `type: repair
authority-id: canonical
brand-id: canonical
repair-id: 2
architectures:
  - amd64
  - arm64
series:
  - 16
models:
  - xyz/frobinator
timestamp: 2017-03-30T12:22:16Z
body-length: 7
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

script


AXNpZw==
`
	testHeadersResp = `{"headers":
{"architectures":["amd64","arm64"],"authority-id":"canonical","body-length":"7","brand-id":"canonical","models":["xyz/frobinator"],"repair-id":"2","series":["16"],"sign-key-sha3-384":"KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj","timestamp":"2017-03-30T12:22:16Z","type":"repair"}}`
)

func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func (r *repairSuite) TestFetchJustRepair(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testRepair)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	a, err := runner.Fetch("canonical", "2")
	c.Assert(err, IsNil)
	c.Check(a, HasLen, 1)
	_, ok := a[0].(*asserts.Repair)
	c.Check(ok, Equals, true)
}

var (
	testRetryStrategy = retry.LimitCount(5, retry.LimitTime(1*time.Second,
		retry.Exponential{
			Initial: 1 * time.Millisecond,
			Factor:  1,
		},
	))
)

func (r *repairSuite) TestFetch500(c *C) {
	restore := repair.MockFetchRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Fetch("canonical", "2")
	c.Assert(err, ErrorMatches, "cannot fetch repair, unexpected status 500")
	c.Assert(n, Equals, 5)
}

func (r *repairSuite) TestFetchEmpty(c *C) {
	restore := repair.MockFetchRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(200)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Fetch("canonical", "2")
	c.Assert(err, Equals, io.ErrUnexpectedEOF)
	c.Assert(n, Equals, 5)
}

func (r *repairSuite) TestFetchBroken(c *C) {
	restore := repair.MockFetchRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(200)
		io.WriteString(w, "xyz:")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Fetch("canonical", "2")
	c.Assert(err, Equals, io.ErrUnexpectedEOF)
	c.Assert(n, Equals, 5)
}

func (r *repairSuite) TestFetchNotFound(c *C) {
	restore := repair.MockFetchRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(404)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Fetch("canonical", "2")
	c.Assert(err, Equals, repair.ErrRepairNotFound)
	c.Assert(n, Equals, 1)
}

func (r *repairSuite) TestFetchIdMismatch(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		io.WriteString(w, testRepair)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Fetch("canonical", "4")
	c.Assert(err, ErrorMatches, `cannot fetch repair, id mismatch canonical/2 != canonical/4`)
}

func (r *repairSuite) TestFetchWrongFirstType(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testKey)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Fetch("canonical", "2")
	c.Assert(err, ErrorMatches, `cannot fetch repair, unexpected first assertion "account-key"`)
}

func (r *repairSuite) TestFetchRepairPlusKey(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testRepair)
		io.WriteString(w, "\n")
		io.WriteString(w, testKey)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	a, err := runner.Fetch("canonical", "2")
	c.Assert(err, IsNil)
	c.Check(a, HasLen, 2)
	_, ok := a[0].(*asserts.Repair)
	c.Check(ok, Equals, true)
	_, ok = a[1].(*asserts.AccountKey)
	c.Check(ok, Equals, true)
}

func (r *repairSuite) TestPeek(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/json")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testHeadersResp)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	h, err := runner.Peek("canonical", "2")
	c.Assert(err, IsNil)
	c.Check(h["series"], DeepEquals, []interface{}{"16"})
	c.Check(h["architectures"], DeepEquals, []interface{}{"amd64", "arm64"})
	c.Check(h["models"], DeepEquals, []interface{}{"xyz/frobinator"})
}

func (r *repairSuite) TestPeek500(c *C) {
	restore := repair.MockPeekRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Peek("canonical", "2")
	c.Assert(err, ErrorMatches, "cannot peek repair headers, unexpected status 500")
	c.Assert(n, Equals, 5)
}

func (r *repairSuite) TestPeekInvalid(c *C) {
	restore := repair.MockPeekRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(200)
		io.WriteString(w, "{")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Peek("canonical", "2")
	c.Assert(err, Equals, io.ErrUnexpectedEOF)
	c.Assert(n, Equals, 5)
}

func (r *repairSuite) TestPeekNotFound(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(404)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Peek("canonical", "2")
	c.Assert(err, Equals, repair.ErrRepairNotFound)
	c.Assert(n, Equals, 1)
}

func (r *repairSuite) TestPeekIdMismatch(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/json")
		io.WriteString(w, testHeadersResp)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Peek("canonical", "4")
	c.Assert(err, ErrorMatches, `cannot peek repair headers, id mismatch canonical/2 != canonical/4`)

}
