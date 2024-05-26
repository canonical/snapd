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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/store"
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

func (s *SnapSuite) TestKnownViaSnapd(c *check.C) {
	n := 0
	expectedQuery := url.Values{
		"series":   []string{"16"},
		"brand-id": []string{"canonical"},
		"model":    []string{"pi99"},
	}

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/assertions/model")
			c.Check(r.URL.Query(), check.DeepEquals, expectedQuery)
			w.Header().Set("X-Ubuntu-Assertions-Count", "1")
			fmt.Fprint(w, mockModelAssertion)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	})

	// first run "normal"
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"known", "model", "series=16", "brand-id=canonical", "model=pi99"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, mockModelAssertion)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 1)

	// then with "--remote"
	n = 0
	s.stdout.Reset()
	expectedQuery["remote"] = []string{"true"}
	rest = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"known", "--remote", "model", "series=16", "brand-id=canonical", "model=pi99"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, mockModelAssertion)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestKnownRemoteViaSnapd(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/assertions/model")
			c.Check(r.URL.Query(), check.DeepEquals, url.Values{
				"series":   []string{"16"},
				"brand-id": []string{"canonical"},
				"model":    []string{"pi99"},
				"remote":   []string{"true"},
			})
			w.Header().Set("X-Ubuntu-Assertions-Count", "1")
			fmt.Fprint(w, mockModelAssertion)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	})

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"known", "--remote", "model", "series=16", "brand-id=canonical", "model=pi99"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, mockModelAssertion)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestKnownRemoteDirect(c *check.C) {
	var server *httptest.Server

	restorer := snap.MockStoreNew(func(cfg *store.Config, stoCtx store.DeviceAndAuthContext) *store.Store {
		if cfg == nil {
			cfg = store.DefaultConfig()
		}
		serverURL, _ := url.Parse(server.URL)
		cfg.AssertionsBaseURL = serverURL
		return store.New(cfg, stoCtx)
	})
	defer restorer()

	n := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, check.Matches, ".*/assertions/.*") // basic check for request
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/assertions/model/16/canonical/pi99")
			fmt.Fprint(w, mockModelAssertion)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	}))

	// first test "--remote --direct"
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"known", "--remote", "--direct", "model", "series=16", "brand-id=canonical", "model=pi99"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, mockModelAssertion)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 1)

	// "--direct" behave the same as "--remote --direct"
	s.stdout.Reset()
	n = 0
	rest = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"known", "--direct", "model", "series=16", "brand-id=canonical", "model=pi99"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, mockModelAssertion)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestKnownRemoteAutoFallback(c *check.C) {
	var server *httptest.Server

	restorer := snap.MockStoreNew(func(cfg *store.Config, stoCtx store.DeviceAndAuthContext) *store.Store {
		if cfg == nil {
			cfg = store.DefaultConfig()
		}
		serverURL, _ := url.Parse(server.URL)
		cfg.AssertionsBaseURL = serverURL
		return store.New(cfg, stoCtx)
	})
	defer restorer()

	n := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, check.Matches, ".*/assertions/.*") // basic check for request
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/assertions/model/16/canonical/pi99")
			fmt.Fprint(w, mockModelAssertion)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	}))

	cli := snap.Client()
	cli.Hijack(func(*http.Request) (*http.Response, error) {
		return nil, client.ConnectionError{Err: fmt.Errorf("no snapd")}
	})

	rest := mylog.Check2(snap.Parser(cli).ParseArgs([]string{"known", "--remote", "model", "series=16", "brand-id=canonical", "model=pi99"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, mockModelAssertion)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestKnownRemoteMissingPrimaryKey(c *check.C) {
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"known", "--remote", "--direct", "model", "series=16", "brand-id=canonical"}))
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
	c.Check(n, check.Equals, 1)
}
