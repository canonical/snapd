// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !integrationcoverage

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

package main_test

import (
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

const findJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
      "developer": "canonical",
      "download-size": 65536,
      "icon": "",
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "name": "hello",
      "private": false,
      "resource": "/v2/snaps/hello",
      "revision": "1",
      "status": "available",
      "summary": "GNU Hello, the \"hello world\" snap",
      "type": "app",
      "version": "2.10"
    },
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "This is a simple hello world example.",
      "developer": "canonical",
      "download-size": 20480,
      "icon": "",
      "id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
      "name": "hello-world",
      "private": false,
      "resource": "/v2/snaps/hello-world",
      "revision": "26",
      "status": "available",
      "summary": "Hello world example",
      "type": "app",
      "version": "6.1"
    },
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "1.0GB",
      "developer": "noise",
      "download-size": 512004096,
      "icon": "",
      "id": "asXOGCreK66DIAdyXmucwspTMgqA4rne",
      "name": "hello-huge",
      "private": false,
      "resource": "/v2/snaps/hello-huge",
      "revision": "1",
      "status": "available",
      "summary": "a really big snap",
      "type": "app",
      "version": "1.0"
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestFind(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, findJson)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"find"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Developer +Notes +Summary
hello +2.10 +canonical +- +GNU Hello, the "hello world" snap
hello-huge +1.0 +noise +- +a really big snap
hello-world +6.1 +canonical +- +Hello world example
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const findHelloJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
      "developer": "canonical",
      "download-size": 65536,
      "icon": "",
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "name": "hello",
      "private": false,
      "resource": "/v2/snaps/hello",
      "revision": "1",
      "status": "available",
      "summary": "GNU Hello, the \"hello world\" snap",
      "type": "app",
      "version": "2.10"
    },
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "1.0GB",
      "developer": "noise",
      "download-size": 512004096,
      "icon": "",
      "id": "asXOGCreK66DIAdyXmucwspTMgqA4rne",
      "name": "hello-huge",
      "private": false,
      "resource": "/v2/snaps/hello-huge",
      "revision": "1",
      "status": "available",
      "summary": "a really big snap",
      "type": "app",
      "version": "1.0"
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestFindHello(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			c.Check(q.Get("q"), check.Equals, "hello")
			fmt.Fprintln(w, findHelloJson)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"find", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Developer +Notes +Summary
hello +2.10 +canonical +- +GNU Hello, the "hello world" snap
hello-huge +1.0 +noise +- +a really big snap
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const findPricedJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
      "developer": "canonical",
      "download-size": 65536,
      "icon": "",
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "name": "hello",
      "prices": {"GBP": 1.99, "USD": 2.99},
      "private": false,
      "resource": "/v2/snaps/hello",
      "revision": "1",
      "status": "priced",
      "summary": "GNU Hello, the \"hello world\" snap",
      "type": "app",
      "version": "2.10"
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestFindPriced(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, findPricedJson)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"find"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Developer +Notes +Summary
hello +2.10 +canonical +1.99GBP +GNU Hello, the "hello world" snap
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const findPricedAndBoughtJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
      "developer": "canonical",
      "download-size": 65536,
      "icon": "",
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "name": "hello",
      "prices": {"GBP": 1.99, "USD": 2.99},
      "private": false,
      "resource": "/v2/snaps/hello",
      "revision": "1",
      "status": "available",
      "summary": "GNU Hello, the \"hello world\" snap",
      "type": "app",
      "version": "2.10"
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestFindPricedAndBought(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, findPricedAndBoughtJson)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"find"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Developer +Notes +Summary
hello +2.10 +canonical +bought +GNU Hello, the "hello world" snap
`)
	c.Check(s.Stderr(), check.Equals, "")
}
