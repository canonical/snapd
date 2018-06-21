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

package main_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"

	"github.com/jessevdk/go-flags"
	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
)

const findJSON = `
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

func (s *SnapSuite) TestFindSnapName(c *check.C) {
	n := 0

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			if q.Get("q") == "" {
				v, ok := q["section"]
				c.Check(ok, check.Equals, true)
				c.Check(v, check.DeepEquals, []string{""})
			}
			fmt.Fprintln(w, findJSON)
		default:
			c.Fatalf("expected to get 2 requests, now on %d", n+1)
		}
		n++
	})

	rest, err := snap.Parser().ParseArgs([]string{"find", "hello"})

	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})

	c.Check(s.Stdout(), check.Matches, `Name +Version +Developer +Notes +Summary
hello +2.10 +canonical +- +GNU Hello, the "hello world" snap
hello-world +6.1 +canonical +- +Hello world example
hello-huge +1.0 +noise +- +a really big snap
`)
	c.Check(s.Stderr(), check.Equals, "")

	s.ResetStdStreams()
}

const findHelloJSON = `
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
			fmt.Fprintln(w, findHelloJSON)
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

const findPricedJSON = `
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
      "version": "2.10",
      "license": "Proprietary"
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
			fmt.Fprintln(w, findPricedJSON)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"find", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Developer +Notes +Summary
hello +2.10 +canonical +1.99GBP +GNU Hello, the "hello world" snap
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const findPricedAndBoughtJSON = `
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
			fmt.Fprintln(w, findPricedAndBoughtJSON)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"find", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `Name +Version +Developer +Notes +Summary
hello +2.10 +canonical +bought +GNU Hello, the "hello world" snap
`)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestFindNothingMeansFeaturedSection(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			c.Check(r.URL.Query().Get("section"), check.Equals, "featured")
			fmt.Fprintln(w, findJSON)
		default:
			c.Fatalf("expected to get 1 request, now on %d", n+1)
		}
		n++
	})

	_, err := snap.Parser().ParseArgs([]string{"find"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestSectionCompletion(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0, 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/sections")
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": []string{"foo", "bar", "baz"},
			})
		default:
			c.Fatalf("expected to get 2 requests, now on #%d", n+1)
		}
		n++
	})

	c.Check(snap.SectionName("").Complete(""), check.DeepEquals, []flags.Completion{
		{Item: "foo"},
		{Item: "bar"},
		{Item: "baz"},
	})

	c.Check(snap.SectionName("").Complete("f"), check.DeepEquals, []flags.Completion{
		{Item: "foo"},
	})
}

const findNetworkTimeoutErrorJSON = `
{
  "type": "error",
  "result": {
    "message": "Get https://search.apps.ubuntu.com/api/v1/snaps/search?confinement=strict%2Cclassic&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha3_384%2Csummary%2Cdescription%2Cdeltas%2Cbinary_filesize%2Cdownload_url%2Cepoch%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Cscreenshot_urls%2Csnap_id%2Csupport_url%2Ccontact%2Ctitle%2Ccontent%2Cversion%2Corigin%2Cdeveloper_id%2Cprivate%2Cconfinement%2Cchannel_maps_list&q=test: net/http: request canceled while waiting for connection (Client.Timeout exceeded while awaiting headers)",
    "kind": "network-timeout"
  },
  "status-code": 400
}`

func (s *SnapSuite) TestFindNetworkTimeoutError(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			fmt.Fprintln(w, findNetworkTimeoutErrorJSON)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	_, err := snap.Parser().ParseArgs([]string{"find", "hello"})
	c.Assert(err, check.ErrorMatches, `unable to contact snap store`)
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *SnapSuite) TestFindSnapSectionOverview(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0, 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/sections")
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": []string{"sec2", "sec1"},
			})
		default:
			c.Fatalf("expected to get 2 requests, now on #%d", n+1)
		}
		n++
	})

	rest, err := snap.Parser().ParseArgs([]string{"find", "--section"})

	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})

	c.Check(s.Stdout(), check.Equals, `No section specified. Available sections:
 * sec1
 * sec2
Please try 'snap find --section=<selected section>'
`)
	c.Check(s.Stderr(), check.Equals, "")

	s.ResetStdStreams()
}

func (s *SnapSuite) TestFindSnapInvalidSection(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/sections")
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": []string{"sec1"},
			})
		default:
			c.Fatalf("expected to get 1 request, now on %d", n+1)
		}

		n++
	})
	_, err := snap.Parser().ParseArgs([]string{"find", "--section=foobar", "hello"})
	c.Assert(err, check.ErrorMatches, `No matching section "foobar", use --section to list existing sections`)
}

func (s *SnapSuite) TestFindSnapNotFoundInSection(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/sections")
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": []string{"foobar"},
			})
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			v, ok := r.URL.Query()["section"]
			c.Check(ok, check.Equals, true)
			c.Check(v, check.DeepEquals, []string{"foobar"})
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": []string{},
			})
		default:
			c.Fatalf("expected to get 2 requests, now on #%d", n+1)
		}
		n++
	})

	_, err := snap.Parser().ParseArgs([]string{"find", "--section=foobar", "hello"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stderr(), check.Equals, "No matching snaps for \"hello\" in section \"foobar\"\n")
	c.Check(s.Stdout(), check.Equals, "")

	s.ResetStdStreams()
}

func (s *SnapSuite) TestFindSnapCachedSection(c *check.C) {
	numHits := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		numHits++
		c.Check(numHits, check.Equals, 1)
		c.Check(r.URL.Path, check.Equals, "/v2/sections")
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": []string{"sec1", "sec2", "sec3"},
		})
	})

	os.MkdirAll(path.Dir(dirs.SnapSectionsFile), 0755)
	ioutil.WriteFile(dirs.SnapSectionsFile, []byte("sec1\nsec2\nsec3"), 0644)

	_, err := snap.Parser().ParseArgs([]string{"find", "--section=foobar", "hello"})
	c.Logf("stdout: %s", s.Stdout())
	c.Assert(err, check.ErrorMatches, `No matching section "foobar", use --section to list existing sections`)

	s.ResetStdStreams()

	rest, err := snap.Parser().ParseArgs([]string{"find", "--section"})

	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})

	c.Check(s.Stdout(), check.Equals, `No section specified. Available sections:
 * sec1
 * sec2
 * sec3
Please try 'snap find --section=<selected section>'
`)

	s.ResetStdStreams()
	c.Check(numHits, check.Equals, 1)
}
