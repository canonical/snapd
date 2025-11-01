// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

type reportProblemSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&reportProblemSuite{})

const mockInfoJSONHelloWithLinks = `
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
      "publisher": {
         "id": "canonical",
         "username": "canonical",
         "display-name": "Canonical",
         "validation": "verified"
      },
      "download-size": 65536,
      "icon": "",
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "name": "hello",
      "private": false,
      "resource": "/v2/snaps/hello",
      "revision": "1",
      "status": "available",
      "summary": "The GNU Hello snap",
      "store-url": "https://snapcraft.io/hello",
      "type": "app",
      "version": "2.10",
      "license": "MIT",
      "channels": {
        "1/stable": {
          "revision": "1",
          "version": "2.10",
          "channel": "1/stable",
          "size": 65536,
          "released-at": "2018-12-18T15:16:56.723501Z"
        }
      },
      "tracks": ["1"],
      "links": {
        "website": ["https://hello.world", "https://snapcraft.io/foo", "https://hello.world"],
        "sources": ["https://github.com/canonical/hello-snap"],
        "contact": ["mailto:store@hello.world"],
        "issues":  ["https://github.com/canonical/hello-snap/issues"]
      }
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

const mockInfoJSONHelloLocalNoLinksLegacyStore = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
      "channel": "stable",
      "confinement": "strict",
      "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
      "developer": "canonical",
      "publisher": {
         "id": "canonical",
         "username": "canonical",
         "display-name": "Canonical",
         "validation": "verified"
      },
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "install-date": "2006-01-02T22:04:07.123456789Z",
      "installed-size": 1024,
      "name": "hello",
      "private": false,
      "revision": "100",
      "status": "available",
      "store-url": "https://snapcraft.io/hello",
      "summary": "The GNU Hello snap",
      "type": "app",
      "version": "2.10",
      "license": "",
      "tracking-channel": "beta"
    }
}
`

func (s *reportProblemSuite) TestReportProblemHappy(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Logf("hit 1")
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			// asks for the instance snap
			c.Check(q.Get("name"), check.Equals, "hello")
			fmt.Fprint(w, mockInfoJSONHelloWithLinks)
		case 1:
			c.Logf("hit 2")
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONWithStoreURL)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"report-problem", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// links are unique and sorted, generic snapcraft.io link is dropped
	c.Check(s.Stdout(), check.Equals, `
Publisher of snap "hello" has listed the following points of contact:
  Contact:
    mailto:local@hello.world
    mailto:store@hello.world
  Issue reporting:
    https://github.com/canonical/hello-snap/issues
  Website:
    https://hello.world

Use one of the links listed above to report a problem with the snap.
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *reportProblemSuite) TestReportProblemNoContact(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			// asks for the instance snap
			c.Check(q.Get("name"), check.Equals, "hello")
			fmt.Fprint(w, mockInfoJSONWithChannels)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			fmt.Fprint(w, mockInfoJSONHelloLocalNoLinksLegacyStore)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"report-problem", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// make sure local and remote info is combined in the output
	c.Check(s.Stdout(), check.Equals, `
Publisher of snap "hello" has not listed any points of contact.
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *reportProblemSuite) TestReportProblemNoLocal(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			// asks for the instance snap
			c.Check(q.Get("name"), check.Equals, "hello")
			fmt.Fprint(w, mockInfoJSONHelloWithLinks)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/hello")
			w.WriteHeader(404)
			fmt.Fprint(w, `{
			"type": "error",
            "status-code": 404,
			"result": {
				"message": "snap not installed",
				"kind": "snap-not-found",
				"value": "hello"
				}}`)
		default:
			c.Fatalf("expected to get 2 requests, now on %d (%v)", n+1, r)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"report-problem", "hello"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	// only store links
	c.Check(s.Stdout(), check.Equals, `
Publisher of snap "hello" has listed the following points of contact:
  Contact:
    mailto:store@hello.world
  Issue reporting:
    https://github.com/canonical/hello-snap/issues
  Website:
    https://hello.world

Use one of the links listed above to report a problem with the snap.
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}
