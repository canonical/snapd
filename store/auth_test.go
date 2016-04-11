// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package store

import (
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/oauth"
	"github.com/ubuntu-core/snappy/osutil"

	. "gopkg.in/check.v1"
)

type authTestSuite struct {
	tempdir string
}

var _ = Suite(&authTestSuite{})

func (s *authTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
}

const mockStoreInvalidLoginCode = 401
const mockStoreInvalidLogin = `
{
    "message": "Provided email/password is not correct.", 
    "code": "INVALID_CREDENTIALS", 
    "extra": {}
}
`

const mockStoreNeeds2faHTTPCode = 401
const mockStoreNeeds2fa = `
{
    "message": "2-factor authentication required.", 
    "code": "TWOFACTOR_REQUIRED", 
    "extra": {}
}
`

const mockStoreReturnToken = `
{
    "openid": "the-open-id-string-that-is-also-the-consumer-key-in-our-store", 
    "token_name": "some-token-name", 
    "date_updated": "2015-02-27T15:00:55.062", 
    "token_key": "the-token-key", 
    "consumer_secret": "the-consumer-secret", 
    "href": "/api/v2/tokens/oauth/something", 
    "date_created": "2015-02-27T14:54:30.863", 
    "consumer_key": "the-consumer-key", 
    "token_secret": "the-token-secret"
}
`

const mockStoreReturnMacaroon = `
{
    "macaroon": "the-root-macaroon-serialized-data"
}
`

const mockStoreReturnDischarge = `
{
    "discharge_macaroon": "the-discharge-macaroon-serialized-data"
}
`

const mockStoreReturnNoMacaroon = `{}`

func (s *authTestSuite) TestRequestStoreToken(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnToken)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	ubuntuoneOauthAPI = mockServer.URL + "/token/oauth"

	token, err := RequestStoreToken("guy@example.com", "passwd", "some-token-name", "")
	c.Assert(err, IsNil)
	c.Assert(token.TokenKey, Equals, "the-token-key")
	c.Assert(token.TokenSecret, Equals, "the-token-secret")
	c.Assert(token.ConsumerSecret, Equals, "the-consumer-secret")
	c.Assert(token.ConsumerKey, Equals, "the-consumer-key")
}

func (s *authTestSuite) TestRequestStoreTokenNeeds2fa(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(mockStoreNeeds2faHTTPCode)
		io.WriteString(w, mockStoreNeeds2fa)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	ubuntuoneOauthAPI = mockServer.URL + "/token/oauth"

	_, err := RequestStoreToken("foo@example.com", "passwd", "some-token-name", "")
	c.Assert(err, Equals, ErrAuthenticationNeeds2fa)
}

func (s *authTestSuite) TestRequestStoreTokenInvalidLogin(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(mockStoreInvalidLoginCode)
		io.WriteString(w, mockStoreInvalidLogin)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	ubuntuoneOauthAPI = mockServer.URL + "/token/oauth"

	_, err := RequestStoreToken("foo@example.com", "passwd", "some-token-name", "")
	c.Assert(err, Equals, ErrInvalidCredentials)
}

func (s *authTestSuite) TestRequestPackageAccessMacaroon(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnMacaroon)
	}))
	defer mockServer.Close()
	MyAppsPackageAccessAPI = mockServer.URL + "/acl/package_access/"

	macaroon, err := RequestPackageAccessMacaroon()
	c.Assert(err, IsNil)
	c.Assert(macaroon, Equals, "the-root-macaroon-serialized-data")
}

func (s *authTestSuite) TestRequestPackageAccessMacaroonMissingData(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoMacaroon)
	}))
	defer mockServer.Close()
	MyAppsPackageAccessAPI = mockServer.URL + "/acl/package_access/"

	macaroon, err := RequestPackageAccessMacaroon()
	c.Assert(err, ErrorMatches, "cannot get package access macaroon from store: empty macaroon returned")
	c.Assert(macaroon, Equals, "")
}

func (s *authTestSuite) TestRequestPackageAccessMacaroonError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer mockServer.Close()
	MyAppsPackageAccessAPI = mockServer.URL + "/acl/package_access/"

	macaroon, err := RequestPackageAccessMacaroon()
	c.Assert(err, ErrorMatches, "cannot get package access macaroon from store: store server returned status 500")
	c.Assert(macaroon, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveat(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnDischarge)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("guy@example.com", "passwd", "root-macaroon", "")
	c.Assert(err, IsNil)
	c.Assert(discharge, Equals, "the-discharge-macaroon-serialized-data")
}

func (s *authTestSuite) TestDischargeAuthCaveatNeeds2fa(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(mockStoreNeeds2faHTTPCode)
		io.WriteString(w, mockStoreNeeds2fa)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("foo@example.com", "passwd", "root-macaroon", "")
	c.Assert(err, Equals, ErrAuthenticationNeeds2fa)
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveatInvalidLogin(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(mockStoreInvalidLoginCode)
		io.WriteString(w, mockStoreInvalidLogin)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("foo@example.com", "passwd", "root-macaroon", "")
	c.Assert(err, ErrorMatches, "cannot get discharge macaroon from store: Provided email/password is not correct.")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveatMissingData(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoMacaroon)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("foo@example.com", "passwd", "root-macaroon", "")
	c.Assert(err, ErrorMatches, "cannot get discharge macaroon from store: empty macaroon returned")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveatError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("foo@example.com", "passwd", "root-macaroon", "")
	c.Assert(err, ErrorMatches, "cannot get discharge macaroon from store: server returned status 500")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestWriteStoreToken(c *C) {
	os.Setenv("HOME", s.tempdir)
	mockStoreToken := StoreToken{TokenName: "meep"}
	err := WriteStoreToken(mockStoreToken)

	c.Assert(err, IsNil)
	outFile := filepath.Join(s.tempdir, "snaps", "snappy", "auth", "sso.json")
	c.Assert(osutil.FileExists(outFile), Equals, true)
	content, err := ioutil.ReadFile(outFile)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
 "openid": "",
 "token_name": "meep",
 "date_updated": "",
 "date_created": "",
 "href": "",
 "token_key": "",
 "token_secret": "",
 "consumer_secret": "",
 "consumer_key": ""
}`)
}

func (s *authTestSuite) TestReadStoreToken(c *C) {
	os.Setenv("HOME", s.tempdir)
	mockStoreToken := StoreToken{
		TokenName: "meep",
		Token: oauth.Token{
			TokenKey:    "token-key",
			TokenSecret: "token-secret",
		},
	}
	err := WriteStoreToken(mockStoreToken)
	c.Assert(err, IsNil)

	readToken, err := ReadStoreToken()
	c.Assert(err, IsNil)
	c.Assert(readToken, DeepEquals, &mockStoreToken)
}
