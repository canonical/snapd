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
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"
)

type authTestSuite struct{}

var _ = Suite(&authTestSuite{})

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

const mockStore2faFailedHTTPCode = 403
const mockStore2faFailedResponse = `
{
    "message": "The provided 2-factor key is not recognised.", 
    "code": "TWOFACTOR_FAILURE", 
    "extra": {}
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

func (s *authTestSuite) TestRequestStoreMacaroon(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnMacaroon)
	}))
	defer mockServer.Close()
	MyAppsMacaroonACLAPI = mockServer.URL + "/acl/"

	macaroon, err := RequestStoreMacaroon()
	c.Assert(err, IsNil)
	c.Assert(macaroon, Equals, "the-root-macaroon-serialized-data")
}

func (s *authTestSuite) TestRequestStoreMacaroonMissingData(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoMacaroon)
	}))
	defer mockServer.Close()
	MyAppsMacaroonACLAPI = mockServer.URL + "/acl/"

	macaroon, err := RequestStoreMacaroon()
	c.Assert(err, ErrorMatches, "cannot get snap access permission from store: empty macaroon returned")
	c.Assert(macaroon, Equals, "")
}

func (s *authTestSuite) TestRequestStoreMacaroonError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer mockServer.Close()
	MyAppsMacaroonACLAPI = mockServer.URL + "/acl/"

	macaroon, err := RequestStoreMacaroon()
	c.Assert(err, ErrorMatches, "cannot get snap access permission from store: store server returned status 500")
	c.Assert(macaroon, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveat(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnDischarge)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("third-party-caveat", "guy@example.com", "passwd", "")
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

	discharge, err := DischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
	c.Assert(err, Equals, ErrAuthenticationNeeds2fa)
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveatFails2fa(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(mockStore2faFailedHTTPCode)
		io.WriteString(w, mockStore2faFailedResponse)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
	c.Assert(err, Equals, Err2faFailed)
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveatInvalidLogin(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(mockStoreInvalidLoginCode)
		io.WriteString(w, mockStoreInvalidLogin)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
	c.Assert(err, ErrorMatches, "cannot authenticate on snap store: Provided email/password is not correct.")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveatMissingData(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoMacaroon)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
	c.Assert(err, ErrorMatches, "cannot authenticate on snap store: empty macaroon returned")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveatError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := DischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
	c.Assert(err, ErrorMatches, "cannot authenticate on snap store: server returned status 500")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestRefreshDischargeMacaroon(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnDischarge)
	}))
	defer mockServer.Close()
	UbuntuoneRefreshDischargeAPI = mockServer.URL + "/tokens/refresh"

	discharge, err := RefreshDischargeMacaroon("soft-expired-serialized-discharge-macaroon")
	c.Assert(err, IsNil)
	c.Assert(discharge, Equals, "the-discharge-macaroon-serialized-data")
}

func (s *authTestSuite) TestRefreshDischargeMacaroonInvalidLogin(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(mockStoreInvalidLoginCode)
		io.WriteString(w, mockStoreInvalidLogin)
	}))
	defer mockServer.Close()
	UbuntuoneRefreshDischargeAPI = mockServer.URL + "/tokens/refresh"

	discharge, err := RefreshDischargeMacaroon("soft-expired-serialized-discharge-macaroon")
	c.Assert(err, ErrorMatches, "cannot authenticate on snap store: Provided email/password is not correct.")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestRefreshDischargeMacaroonMissingData(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoMacaroon)
	}))
	defer mockServer.Close()
	UbuntuoneRefreshDischargeAPI = mockServer.URL + "/tokens/refresh"

	discharge, err := RefreshDischargeMacaroon("soft-expired-serialized-discharge-macaroon")
	c.Assert(err, ErrorMatches, "cannot authenticate on snap store: empty macaroon returned")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestRefreshDischargeMacaroonError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer mockServer.Close()
	UbuntuoneRefreshDischargeAPI = mockServer.URL + "/tokens/refresh"

	discharge, err := RefreshDischargeMacaroon("soft-expired-serialized-discharge-macaroon")
	c.Assert(err, ErrorMatches, "cannot authenticate on snap store: server returned status 500")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestMacaroonSerialize(c *C) {
	m, err := makeTestMacaroon()
	c.Check(err, IsNil)

	serialized, err := MacaroonSerialize(m)
	c.Check(err, IsNil)

	deserialized, err := MacaroonDeserialize(serialized)
	c.Check(err, IsNil)
	c.Check(deserialized, DeepEquals, m)
}

func (s *authTestSuite) TestMacaroonSerializeDeserializeStoreMacaroon(c *C) {
	// sample serialized macaroon using store server setup.
	serialized := `MDAxNmxvY2F0aW9uIGxvY2F0aW9uCjAwMTdpZGVudGlmaWVyIHNvbWUgaWQKMDAwZmNpZCBjYXZlYXQKMDAxOWNpZCAzcmQgcGFydHkgY2F2ZWF0CjAwNTF2aWQgcyvpXSVlMnj9wYw5b-WPCLjTnO_8lVzBrRr8tJfu9tOhPORbsEOFyBwPOM_YiiXJ_qh-Pp8HY0HsUueCUY4dxONLIxPWTdMzCjAwMTJjbCByZW1vdGUuY29tCjAwMmZzaWduYXR1cmUgcm_Gdz75wUCWF9KGXZQEANhwfvBcLNt9xXGfAmxurPMK`

	deserialized, err := MacaroonDeserialize(serialized)
	c.Check(err, IsNil)

	// expected json serialization of the above macaroon
	jsonData := []byte(`{"caveats":[{"cid":"caveat"},{"cid":"3rd party caveat","vid":"cyvpXSVlMnj9wYw5b-WPCLjTnO_8lVzBrRr8tJfu9tOhPORbsEOFyBwPOM_YiiXJ_qh-Pp8HY0HsUueCUY4dxONLIxPWTdMz","cl":"remote.com"}],"location":"location","identifier":"some id","signature":"726fc6773ef9c1409617d2865d940400d8707ef05c2cdb7dc5719f026c6eacf3"}`)

	var expected macaroon.Macaroon
	err = expected.UnmarshalJSON(jsonData)
	c.Check(err, IsNil)
	c.Check(deserialized, DeepEquals, &expected)

	// reserializing the macaroon should give us the same original store serialization
	reserialized, err := MacaroonSerialize(deserialized)
	c.Check(err, IsNil)
	c.Check(reserialized, Equals, serialized)
}

func (s *authTestSuite) TestMacaroonDeserializeInvalidData(c *C) {
	serialized := "invalid-macaroon-data"

	deserialized, err := MacaroonDeserialize(serialized)
	c.Check(deserialized, IsNil)
	c.Check(err, NotNil)
}

func (s *authTestSuite) TestLoginCaveatIDReturnCaveatID(c *C) {
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	c.Check(err, IsNil)
	err = m.AddThirdPartyCaveat([]byte("shared-key"), "third-party-caveat", UbuntuoneLocation)
	c.Check(err, IsNil)

	caveat, err := LoginCaveatID(m)
	c.Check(err, IsNil)
	c.Check(caveat, Equals, "third-party-caveat")
}

func (s *authTestSuite) TestLoginCaveatIDMacaroonMissingCaveat(c *C) {
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	c.Check(err, IsNil)
	err = m.AddThirdPartyCaveat([]byte("shared-key"), "third-party-caveat", "other-location")
	c.Check(err, IsNil)

	caveat, err := LoginCaveatID(m)
	c.Check(err, NotNil)
	c.Check(caveat, Equals, "")
}
