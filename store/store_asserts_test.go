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
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/store"
)

type storeAssertsSuite struct {
	baseStoreSuite

	storeSigning *assertstest.StoreStack
	dev1Acct     *asserts.Account
	decl1        *asserts.SnapDeclaration

	db *asserts.Database
}

var _ = Suite(&storeAssertsSuite{})

func (s *storeAssertsSuite) SetUpTest(c *C) {
	s.baseStoreSuite.SetUpTest(c)

	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)
	s.dev1Acct = assertstest.NewAccount(s.storeSigning, "developer1", map[string]interface{}{
		"account-id": "developer1",
	}, "")

	a, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "asnapid",
		"snap-name":    "asnap",
		"publisher-id": "developer1",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.decl1 = a.(*asserts.SnapDeclaration)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	s.db = db
}

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

func (s *storeAssertsSuite) TestDownloadAssertionsSimple(c *C) {
	assertstest.AddMany(s.db, s.storeSigning.StoreAccountKey(""), s.dev1Acct)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/assertions/.*")
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/snap-declaration/16/asnapid")
		c.Check(r.URL.Query().Get("max-format"), Equals, "88")
		w.Write(asserts.Encode(s.decl1))
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}

	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	streamURL, err := mockServerURL.Parse("/assertions/snap-declaration/16/asnapid")
	c.Assert(err, IsNil)
	urls := []string{streamURL.String() + "?max-format=88"}

	b := asserts.NewBatch(nil)
	err = sto.DownloadAssertions(urls, b, nil)
	c.Assert(err, IsNil)

	c.Assert(b.CommitTo(s.db, nil), IsNil)

	// added
	_, err = s.decl1.Ref().Resolve(s.db.Find)
	c.Check(err, IsNil)
}

func (s *storeAssertsSuite) TestDownloadAssertionsWithStreams(c *C) {
	stream1 := append(asserts.Encode(s.decl1), "\n"...)
	stream1 = append(stream1, asserts.Encode(s.dev1Acct)...)
	stream2 := asserts.Encode(s.storeSigning.StoreAccountKey(""))

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/assertions/.*")

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		var stream []byte
		switch r.URL.Path {
		case "/assertions/stream1":
			stream = stream1
		case "/assertions/stream2":
			stream = stream2
		default:
			c.Fatal("unexpected stream url")
		}

		w.Write(stream)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}

	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	stream1URL, err := mockServerURL.Parse("/assertions/stream1")
	c.Assert(err, IsNil)
	stream2URL, err := mockServerURL.Parse("/assertions/stream2")
	c.Assert(err, IsNil)

	urls := []string{stream1URL.String(), stream2URL.String()}

	b := asserts.NewBatch(nil)
	err = sto.DownloadAssertions(urls, b, nil)
	c.Assert(err, IsNil)

	c.Assert(b.CommitTo(s.db, nil), IsNil)

	// added
	_, err = s.decl1.Ref().Resolve(s.db.Find)
	c.Check(err, IsNil)
}

func (s *storeAssertsSuite) TestDownloadAssertionsBrokenStream(c *C) {
	stream1 := append(asserts.Encode(s.decl1), "\n"...)
	stream1 = append(stream1, asserts.Encode(s.dev1Acct)...)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/assertions/stream1")

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")

		breakAt := bytes.Index(stream1, []byte("account-id"))
		w.Write(stream1[:breakAt])
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}

	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	stream1URL, err := mockServerURL.Parse("/assertions/stream1")
	c.Assert(err, IsNil)

	urls := []string{stream1URL.String()}

	b := asserts.NewBatch(nil)
	err = sto.DownloadAssertions(urls, b, nil)
	c.Assert(err, Equals, io.ErrUnexpectedEOF)
}

func (s *storeAssertsSuite) TestDownloadAssertions500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/assertions/stream1")

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")

		n++
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}

	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	stream1URL, err := mockServerURL.Parse("/assertions/stream1")
	c.Assert(err, IsNil)

	urls := []string{stream1URL.String()}

	b := asserts.NewBatch(nil)
	err = sto.DownloadAssertions(urls, b, nil)
	c.Assert(err, ErrorMatches, `cannot download assertion stream: got unexpected HTTP status code 500 via .+`)
	c.Check(n, Equals, 5)
}

func (s *storeAssertsSuite) TestDownloadAssertionsStreamNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/assertions/stream1")

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		io.WriteString(w, `{"status": 404,"title": "not found"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}

	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	stream1URL, err := mockServerURL.Parse("/assertions/stream1")
	c.Assert(err, IsNil)

	urls := []string{stream1URL.String()}

	b := asserts.NewBatch(nil)
	err = sto.DownloadAssertions(urls, b, nil)
	c.Assert(err, ErrorMatches, `assertion service error: \[not found\].*`)
}
