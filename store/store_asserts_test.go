// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package store_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/store"
)

type storeAssertsSuite struct {
	baseStoreSuite
}

var _ = Suite(&storeAssertsSuite{})

var testAssertion = `type: snap-declaration
authority-id: super
series: 16
snap-id: snapidfoo
publisher-id: devidbaz
snap-name: mysnap
timestamp: 2016-03-30T12:22:16Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

openpgp wsBcBAABCAAQBQJW+8VBCRDWhXkqAWcrfgAAQ9gIABZFgMPByJZeUE835FkX3/y2hORn
AzE3R1ktDkQEVe/nfVDMACAuaw1fKmUS4zQ7LIrx/AZYw5i0vKVmJszL42LBWVsqR0+p9Cxebzv9
U2VUSIajEsUUKkBwzD8wxFzagepFlScif1NvCGZx0vcGUOu0Ent0v+gqgAv21of4efKqEW7crlI1
T/A8LqZYmIzKRHGwCVucCyAUD8xnwt9nyWLgLB+LLPOVFNK8SR6YyNsX05Yz1BUSndBfaTN8j/k8
8isKGZE6P0O9ozBbNIAE8v8NMWQegJ4uWuil7D3psLkzQIrxSypk9TrQ2GlIG2hJdUovc5zBuroe
xS4u9rVT6UY=`

func (s *storeAssertsSuite) TestAssertion(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 88)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/api/v1/snaps/assertions/.*")
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/snap-declaration/16/snapidfoo")
		c.Check(r.URL.Query().Get("max-format"), Equals, "88")
		io.WriteString(w, testAssertion)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	a, err := sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
}

func (s *storeAssertsSuite) TestAssertionProxyStoreFromAuthContext(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 88)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/api/v1/snaps/assertions/.*")
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/snap-declaration/16/snapidfoo")
		c.Check(r.URL.Query().Get("max-format"), Equals, "88")
		io.WriteString(w, testAssertion)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	nowhereURL, err := url.Parse("http://nowhere.invalid")
	c.Assert(err, IsNil)
	cfg := store.Config{
		AssertionsBaseURL: nowhereURL,
	}
	dauthCtx := &testDauthContext{
		c:             c,
		device:        s.device,
		proxyStoreID:  "foo",
		proxyStoreURL: mockServerURL,
	}
	sto := store.New(&cfg, dauthCtx)

	a, err := sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
}

func (s *storeAssertsSuite) TestAssertionNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/api/v1/snaps/assertions/.*")
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/snap-declaration/16/snapidfoo")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		io.WriteString(w, `{"status": 404,"title": "not found"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		AssertionsBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	_, err := sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Check(asserts.IsNotFound(err), Equals, true)
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.SnapDeclarationType,
		Headers: map[string]string{
			"series":  "16",
			"snap-id": "snapidfoo",
		},
	})
}

func (s *storeAssertsSuite) TestAssertion500(c *C) {
	var n = 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/api/v1/snaps/assertions/.*")
		n++
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		AssertionsBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	_, err := sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, ErrorMatches, `cannot fetch assertion: got unexpected HTTP status code 500 via .+`)
	c.Assert(n, Equals, 5)
}
