// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/store"
)

var statement = []byte(fmt.Sprintf(`{"type": "snap-build",
"authority-id": "devel1",
"series": "16",
"snap-id": "snapidsnapidsnapidsnapidsnapidsn",
"snap-sha3-384": "QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL",
"snap-size": "1",
"grade": "devel",
"timestamp": %q
}`, time.Now().Format(time.RFC3339)))

func (s *SnapKeysSuite) TestHappyDefaultKey(c *C) {
	s.stdin.Write(statement)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	a, err := asserts.Decode(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapBuildType)

	c.Check(s.stderr.String(), Equals, "WARNING: could not fetch account-key to cross-check signed assertion with key constraints.\n")
}

func (s *SnapKeysSuite) TestHappyNonDefaultKey(c *C) {
	s.stdin.Write(statement)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign", "-k", "another"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	a, err := asserts.Decode(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapBuildType)
}

const mockAccountKeyAssertion = `type: account-key
authority-id: canonical
public-key-sha3-384: g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ
account-id: devel1
name: default
since: 2020-01-01T00:00:00Z
body-length: 717
sign-key-sha3-384: -CvQKAwRQ5h3Ffn10FILJoEZUXOv6km9FwA80-Rcj-f-6jadQ89VRswHNiEB9Lxk

AcbBTQRWhcGAARAA3z0Hq999YPQt4TgXjYd4YKoju4b7PbALdMANr9Kiddosp5clXXatNRt8ncfT
Q63cI2qIIpWMjqLdrd013EG6eXc+eAD8J/Cl6gDqj8q+wcPp9KybqmTlZFOO8jZxjnYG77ZSrL+x
9GzYWZwMtZjP0IlrymOL2eT+SARNo/cGq83+U5Gv82r3AFUDQSPZwMsb8Z8FqVRJx6Gs4jpTi/7E
P+X9DYC7J02EMehGJh+L1q5SkeQxPHkAIsOLrdw23BsxMWu4jv+ld9sWb0Znv1oJ0grpg/Y9I6lW
i9AI4cT6c+j12jEatapw5FHKv9xrp+HBrc5cRTjpY2gWWUHNx8TiKsUBe1V4Lgoy+qxDnf5u9jjH
dc5G1YXFAd4XqENtVtELUOvgV5IQ4fMgu0UrAdO6AQaLLvFVYqXLxQdl/i5I+B5jGTkwjwwcaFq5
BYltEOIJK1XVaY4sfqhdijibgJq3mLdwHWoZlSLS1ZxQKtJSpp4MM6HK+sOTAnoerVsb9UNhqxH4
yh0p+CiwTBWV1uV5uLsVlpra5kMaZgSotAY8AxTeYLxQnGn5QlzKfpVL5/iUaX6CdgLZ+DEADBQ4
2M51oL9etuP63KA1pxBpDhPFDiJRAXss6KgXMzB/l7s72634+hNDngYA1yWQM0scomp0J0c2J9OV
k7cbdw70UJm3u3EAEQEAAQ==

AXNpZw==
`

const mockAccountAssertion = `type: account
authority-id: canonical
account-id: devel1
display-name: Developer One
username: devel1
validation: verified
timestamp: 2020-01-01T00:00:00Z
body-length: 0
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==
`

func (s *SnapKeysSuite) checkSignChainResults(c *C, statementType *asserts.AssertionType) {
	dec := asserts.NewDecoder(s.stdout)

	numAsserts := 0

	foundStatement, foundAccountKey, foundAccount := false, false, false

	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		c.Assert(err, IsNil)
		switch a.Type() {
		case asserts.AccountType:
			foundAccount = true
		case asserts.AccountKeyType:
			foundAccountKey = true
		case statementType:
			foundStatement = true
		default:
			c.Fatalf("expected %s, account-key, and account asserts, got %q", statementType.Name, a.Type().Name)
		}
		numAsserts++
	}

	c.Assert(numAsserts, Equals, 3)

	c.Assert(foundStatement, Equals, true)
	c.Assert(foundAccountKey, Equals, true)
	c.Assert(foundAccount, Equals, true)
}

func (s *SnapKeysSuite) TestSignChain(c *C) {
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
		c.Assert(r.URL.Path, Matches, ".*/assertions/.*") // basic check for request
		switch n {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/assertions/account-key/g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ")
			fmt.Fprint(w, mockAccountKeyAssertion)
		case 1:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/assertions/account/devel1")
			fmt.Fprint(w, mockAccountAssertion)
		default:
			c.Fatalf("expected to get 2 requests, now on %d", n+1)
		}
		n++
	}))

	s.stdin.Write([]byte(statement))
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign", "--chain"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	s.checkSignChainResults(c, asserts.SnapBuildType)

	c.Check(s.stderr.String(), HasLen, 0)
}

func (s *SnapKeysSuite) TestSignChainUnknownAccountOrKey(c *C) {
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

	// case 1, fail on account-key
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, Matches, ".*/assertions/.*") // basic check for request
		switch r.URL.Path {
		case "/v2/assertions/account-key/g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ":
			c.Check(r.Method, Equals, "GET")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			io.WriteString(w, `{"error-list":[{"code":"not-found","message":"not found: no ..."}]}`)
		default:
			c.Fatalf("Unexpected %s request for %s", r.Method, r.URL.Path)
		}
	}))

	s.stdin.Write([]byte(statement))
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign", "--chain"})
	c.Assert(err, ErrorMatches, "cannot create assertion chain: account-key .* not found")
	// if we fail in retrieving the account-key assertion, we should not write
	// partial output
	c.Assert(s.Stdout(), Equals, "")

	// case 2, fail on account
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, Matches, ".*/assertions/.*") // basic check for request
		switch r.URL.Path {
		case "/v2/assertions/account-key/g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ":
			c.Check(r.Method, Equals, "GET")
			fmt.Fprint(w, mockAccountKeyAssertion)
		case "/v2/assertions/account/devel1":
			c.Check(r.Method, Equals, "GET")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			io.WriteString(w, `{"error-list":[{"code":"not-found","message":"not found: no ..."}]}`)
		default:
			c.Fatalf("Unexpected %s request for %s", r.Method, r.URL.Path)
		}
	}))

	s.stdin.Reset()
	s.stdout.Reset()
	s.stdin.Write([]byte(statement))
	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"sign", "--chain"})
	c.Assert(err, ErrorMatches, "cannot create assertion chain: account .* not found")
	// if we fail in retrieving the account assertion, we should not write
	// partial output
	c.Assert(s.Stdout(), Equals, "")
}
