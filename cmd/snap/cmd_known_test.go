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
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/jessevdk/go-flags"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"

	snap "github.com/snapcore/snapd/cmd/snap"
)

// acquire example data via:
// curl  -H "accept: application/x.ubuntu.assertion" https://assertions.ubuntu.com/v1/assertions/model/16/canonical/pi2
const mockModelAssertion = `type: model
authority-id: canonical
series: 16
brand-id: canonical
model: pi99
architecture: armhf
gadget: pi99
kernel: pi99-kernel
timestamp: 2016-08-31T00:00:00.0Z
sign-key-sha3-384: 9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn

AcLorsomethingthatlooksvaguelylikeasignature==
`

func (s *SnapSuite) TestKnownRemote(c *check.C) {
	var server *httptest.Server

	restorer := snap.MockStoreNew(func(cfg *store.Config, auth auth.AuthContext) *store.Store {
		if cfg == nil {
			cfg = store.DefaultConfig()
		}
		serverURL, _ := url.Parse(server.URL)
		cfg.AssertionsBaseURL = serverURL
		return store.New(cfg, auth)
	})
	defer restorer()

	n := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, check.Matches, ".*/assertions/.*") // sanity check request
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/api/v1/snaps/assertions/model/16/canonical/pi99")
			fmt.Fprintln(w, mockModelAssertion)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	}))

	rest, err := snap.Parser().ParseArgs([]string{"known", "--remote", "model", "series=16", "brand-id=canonical", "model=pi99"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, mockModelAssertion)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestKnownRemoteMissingPrimaryKey(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"known", "--remote", "model", "series=16", "brand-id=canonical"})
	c.Assert(err, check.ErrorMatches, `cannot query remote assertion: must provide primary key: model`)
}

func (s *SnapSuite) TestAssertTypeNameCompletion(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/assertions")
			fmt.Fprintln(w, `{"type": "sync", "result": { "types": [ "account", "... more stuff ...", "validation" ] } }`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})

	c.Check(snap.AssertTypeNameCompletion("v"), check.DeepEquals, []flags.Completion{{Item: "validation"}})
}
