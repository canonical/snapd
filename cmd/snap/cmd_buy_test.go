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
	"encoding/json"
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestBuyHelp(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"buy"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "the required argument `<snap-name>` was not provided")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestBuyInvalidCharacters(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"buy", "a:b"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "cannot buy snap \"a:b\": invalid characters in name")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")

	_, err = snap.Parser().ParseArgs([]string{"buy", "c*d"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "cannot buy snap \"c*d\": invalid characters in name")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

const buyFreeSnapFailsFindJson = `
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
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestBuyFreeSnapFails(c *check.C) {
	getCount := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			c.Check(q.Get("q"), check.Equals, "name:hello")
			fmt.Fprintln(w, buyFreeSnapFailsFindJson)
			getCount++
		default:
			c.Fatalf("unexpected HTTP method %q", r.Method)
		}
	})
	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "cannot buy snap \"hello\": snap is free")
	c.Assert(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(getCount, check.Equals, 1)
}

const buySnapFindJson = `
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
      "status": "priced",
      "summary": "GNU Hello, the \"hello world\" snap",
      "type": "app",
      "version": "2.10",
      "prices": {"USD": 3.99, "GBP": 2.99}
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

const buySnapJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "snap_id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
    "refundable_until": "2015-07-15 18:46:21",
    "state": "Complete"
  },
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestBuySnap(c *check.C) {
	getCount := 0
	postCount := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			c.Check(q.Get("q"), check.Equals, "name:hello")
			fmt.Fprintln(w, buySnapFindJson)
			getCount++
		case "POST":
			c.Check(r.URL.Path, check.Equals, "/v2/buy")

			var postData struct {
				SnapID   string  `json:"snap-id"`
				SnapName string  `json:"snap-name"`
				Channel  string  `json:"channel"`
				Price    float64 `json:"price"`
				Currency string  `json:"currency"`
			}
			decoder := json.NewDecoder(r.Body)
			err := decoder.Decode(&postData)
			c.Assert(err, check.IsNil)

			c.Check(postData.SnapID, check.Equals, "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6")
			c.Check(postData.SnapName, check.Equals, "hello")
			c.Check(postData.Channel, check.Equals, "stable")
			c.Check(postData.Price, check.Equals, 2.99)
			c.Check(postData.Currency, check.Equals, "GBP")

			fmt.Fprintln(w, buySnapJson)
			postCount++
		default:
			c.Fatalf("unexpected HTTP method %q", r.Method)
		}
	})

	fmt.Fprint(s.stdin, "y\n")

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Check(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "Do you want to buy \"hello\" from \"canonical\" for 2.99GBP? (Y/n): hello bought\n")
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(getCount, check.Equals, 1)
	c.Check(postCount, check.Equals, 1)
}

func (s *SnapSuite) TestBuyCancel(c *check.C) {
	getCount := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			c.Check(r.URL.Path, check.Equals, "/v2/find")
			q := r.URL.Query()
			c.Check(q.Get("q"), check.Equals, "name:hello")
			fmt.Fprintln(w, buySnapFindJson)
			getCount++
		default:
			c.Fatalf("unexpected HTTP method %q", r.Method)
		}
	})

	fmt.Fprint(s.stdin, "no\n")

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "aborting")
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, "Do you want to buy \"hello\" from \"canonical\" for 2.99GBP? (Y/n): ")
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(getCount, check.Equals, 1)
}
