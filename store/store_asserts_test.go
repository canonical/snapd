// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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

	a := mylog.Check2(s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "asnapid",
		"snap-name":    "asnap",
		"publisher-id": "developer1",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	s.decl1 = a.(*asserts.SnapDeclaration)

	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	}))

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

func (s *storeAssertsSuite) testAssertion(c *C, assertionMaxFormats map[string]int) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/v2/assertions/.*")
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

	if assertionMaxFormats != nil {
		sto.SetAssertionMaxFormats(assertionMaxFormats)
	}

	a := mylog.Check2(sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil))

	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
}

func (s *storeAssertsSuite) TestAssertion(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 88)
	defer restore()

	s.testAssertion(c, nil)
}

func (s *storeAssertsSuite) TestAssertionSetAssertionMaxFormats(c *C) {
	s.testAssertion(c, map[string]int{
		"snap-declaration": 88,
	})
}

var testAssertionOptionalPrimaryKeys = `type: snap-revision
authority-id: super
snap-sha3-384: QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL
snap-id: snap-id-1
snap-size: 123
snap-revision: 1
developer-id: dev-id1
timestamp: 2022-02-25T12:22:16Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`

func (s *storeAssertsSuite) TestAssertionReducedPrimaryKey(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapRevisionType, 88)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/v2/assertions/.*")
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/snap-revision/QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL")
		c.Check(r.URL.Query().Get("max-format"), Equals, "88")
		io.WriteString(w, testAssertionOptionalPrimaryKeys)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	a := mylog.Check2(sto.Assertion(asserts.SnapRevisionType, []string{"QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL", "global-upload"}, nil))

	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapRevisionType)
	c.Check(a.HeaderString("provenance"), Equals, "global-upload")
}

func (s *storeAssertsSuite) TestAssertionProxyStoreFromAuthContext(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 88)
	defer restore()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/v2/assertions/.*")
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
	nowhereURL := mylog.Check2(url.Parse("http://nowhere.invalid"))

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

	a := mylog.Check2(sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil))

	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
}

func (s *storeAssertsSuite) TestAssertionNotFoundV1(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	_ := mylog.Check2(sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil))
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.SnapDeclarationType,
		Headers: map[string]string{
			"series":  "16",
			"snap-id": "snapidfoo",
		},
	})
}

func (s *storeAssertsSuite) TestAssertionNotFoundV2(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/v2/assertions/.*")
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/snap-declaration/16/snapidfoo")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		io.WriteString(w, `{"error-list":[{"code":"not-found","message":"not found: no ..."}]}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		AssertionsBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	_ := mylog.Check2(sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil))
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.SnapDeclarationType,
		Headers: map[string]string{
			"series":  "16",
			"snap-id": "snapidfoo",
		},
	})
}

func (s *storeAssertsSuite) TestAssertion500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/v2/assertions/.*")
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

	_ := mylog.Check2(sto.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil))
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

	streamURL := mylog.Check2(mockServerURL.Parse("/assertions/snap-declaration/16/asnapid"))

	urls := []string{streamURL.String() + "?max-format=88"}

	b := asserts.NewBatch(nil)
	mylog.Check(sto.DownloadAssertions(urls, b, nil))


	c.Assert(b.CommitTo(s.db, nil), IsNil)

	// added
	_ = mylog.Check2(s.decl1.Ref().Resolve(s.db.Find))
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

	stream1URL := mylog.Check2(mockServerURL.Parse("/assertions/stream1"))

	stream2URL := mylog.Check2(mockServerURL.Parse("/assertions/stream2"))


	urls := []string{stream1URL.String(), stream2URL.String()}

	b := asserts.NewBatch(nil)
	mylog.Check(sto.DownloadAssertions(urls, b, nil))


	c.Assert(b.CommitTo(s.db, nil), IsNil)

	// added
	_ = mylog.Check2(s.decl1.Ref().Resolve(s.db.Find))
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

	stream1URL := mylog.Check2(mockServerURL.Parse("/assertions/stream1"))


	urls := []string{stream1URL.String()}

	b := asserts.NewBatch(nil)
	mylog.Check(sto.DownloadAssertions(urls, b, nil))
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

	stream1URL := mylog.Check2(mockServerURL.Parse("/assertions/stream1"))


	urls := []string{stream1URL.String()}

	b := asserts.NewBatch(nil)
	mylog.Check(sto.DownloadAssertions(urls, b, nil))
	c.Assert(err, ErrorMatches, `cannot download assertion stream: got unexpected HTTP status code 500 via .+`)
	c.Check(n, Equals, 5)
}

func (s *storeAssertsSuite) TestDownloadAssertionsStreamNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/assertions/stream1")

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		io.WriteString(w, `{"error-list":[{"code":"not-found","message":"not found: no ..."}]}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		StoreBaseURL: mockServerURL,
	}

	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&cfg, dauthCtx)

	stream1URL := mylog.Check2(mockServerURL.Parse("/assertions/stream1"))


	urls := []string{stream1URL.String()}

	b := asserts.NewBatch(nil)
	mylog.Check(sto.DownloadAssertions(urls, b, nil))
	c.Assert(err, ErrorMatches, `assertion service error: \"not found.*`)
}

var testValidationSetAssertion = `type: validation-set
authority-id: K9scFPuK62alndiUfSxJte9WSeZihEcD
series: 16
account-id: K9scFPuK62alndiUfSxJte9WSeZihEcD
name: set-1
sequence: 2
snaps:
  -
    id: yOqKhntON3vR7kwEbVPsILm7bUViPDzz
    name: lxd
    presence: optional
    revision: 1
  -
    id: andRelFqGSFNzJRfWD6SEL34YzEfEEiR
    name: jq
    presence: optional
  -
    id: UP3QB9yet9QvNhXcCZVdgL1VleVaqz8V
    name: suligap-python3-qrcode
    revision: 4
timestamp: 2020-11-06T09:16:26Z
sign-key-sha3-384: 4sq0NF2nUf53bg-G3AXBs0Paj73IYg4g1kWpBEVaAnzh1eNQEI2-UVeFz4e1MEUW

AcLBcwQAAQoAHRYhBA519PIIp64v4+mi5pz1bjeveYQwBQJfpRRqAAoJEJz1bjeveYQwIHUP/A6z
51knc4y/hYF/aAbrea1VFBxddu7BW18w4J97QDWJOah+TT7HMbvduEneeTEPNl9fO8CUtqUSV5JH
GO5WmcS8gHMELMRz7deMKkwzHU1tL7G3xAqIP5ctkNDhobJyCQmU8yyJdp2e6dw5RVFBE9WcAlpO
bRhYIFIUUO0Fn6XvKZuDvCFC3rzRmQV/taAR0jYTbHgeOirr8loEfTKKQZQOaE2GyA5cl0vzx3UT
5uct/giBHDNXFocHEpw/1wwUkqZgOGkT3/tuyiYd0HQ5jdTDldHs9EPRIcwTEjjFtseBUr9W5m/a
kFkWBWPe5FkLvC74H8WXUQbQHgii6RxDnJ1bBVzCOH65pgtRWNCTcoYr5sEB2tPEFEh50bha+37Q
1c3lvGGQWyQRz5uxE5aZNiTaLdnQxPEF+nFd1yTwh7yR8Gqv/SuQMxS/AMQz/3sltfssOjayOtV1
N2R8HGUVKutoRGWMp+YmGO68wHjk5Ff9cIQvXfDviSl4KezrDIIFRqx0ZJaYh1FDmOTfAK68yEFu
P8aWCC2W3HIrdx2mnikT3oVf6yN1KSY5qCE2xdhyyKtt+4y5ZJdQK6JxzTanzh4PZVdiPIUhDv4r
AeDBddPc+mqQtb8bpZ7hMD+dA/B4dA3cRl44Nb/5KcfKjdvl7qpmJQl88OA3DOMpXuxmrrVA
`

func (s *storeAssertsSuite) testSeqFormingAssertion(c *C, assertionMaxFormats map[string]int) {
	// overwritten by test loop for each test case
	expectedSeqArg := "sample"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/v2/assertions/.*")
		// check device authorization is set, implicitly checking doRequest was used
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="device-macaroon"`)

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/validation-set/16/account-foo/set-bar")
		q := r.URL.Query()
		c.Check(q.Get("sequence"), Equals, expectedSeqArg)
		c.Check(q.Get("max-format"), Equals, "88")
		io.WriteString(w, testValidationSetAssertion)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	for _, tc := range []struct {
		sequenceKey    []string
		sequence       int
		expectedSeqArg string
	}{
		{[]string{"16", "account-foo", "set-bar"}, 2, "2"},
		{[]string{"16", "account-foo", "set-bar"}, 0, "latest"},
	} {
		expectedSeqArg = tc.expectedSeqArg

		mockServerURL, _ := url.Parse(mockServer.URL)
		cfg := store.Config{
			StoreBaseURL: mockServerURL,
		}
		dauthCtx := &testDauthContext{c: c, device: s.device}
		sto := store.New(&cfg, dauthCtx)

		if assertionMaxFormats != nil {
			sto.SetAssertionMaxFormats(assertionMaxFormats)
		}

		a := mylog.Check2(sto.SeqFormingAssertion(asserts.ValidationSetType, tc.sequenceKey, tc.sequence, nil))

		c.Check(a, NotNil)
		c.Check(a.Type(), Equals, asserts.ValidationSetType)
	}
}

func (s *storeAssertsSuite) TestSeqFormingAssertion(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.ValidationSetType, 88)
	defer restore()

	s.testSeqFormingAssertion(c, nil)
}

func (s *storeAssertsSuite) TestSeqFormingAssertionSetAssertionMaxFormats(c *C) {
	s.testSeqFormingAssertion(c, map[string]int{
		"validation-set": 88,
	})
}

func (s *storeAssertsSuite) TestSeqFormingAssertionNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "GET", "/v2/assertions/.*")
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Matches, ".*/validation-set/16/account-foo/set-bar")
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		io.WriteString(w, `{"error-list":[{"code":"not-found","message":"not found: no ..."}]}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	cfg := store.Config{
		AssertionsBaseURL: mockServerURL,
	}
	sto := store.New(&cfg, nil)

	_ := mylog.Check2(sto.SeqFormingAssertion(asserts.ValidationSetType, []string{"16", "account-foo", "set-bar"}, 1, nil))
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.ValidationSetType,
		Headers: map[string]string{
			"series":     "16",
			"account-id": "account-foo",
			"name":       "set-bar",
			"sequence":   "1",
		},
	})

	// latest requested
	_ = mylog.Check2(sto.SeqFormingAssertion(asserts.ValidationSetType, []string{"16", "account-foo", "set-bar"}, 0, nil))
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.ValidationSetType,
		Headers: map[string]string{
			"series":     "16",
			"account-id": "account-foo",
			"name":       "set-bar",
		},
	})
}
