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
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
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

const mockStoreReturnNonce = `
{
    "nonce": "the-opaque-nonce"
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

func makeMockedService(handler http.HandlerFunc) (*httptest.Server, *SnapUbuntuStoreAuthService) {
	mockServer := httptest.NewServer(handler)
	url, err := url.Parse(mockServer.URL + "/identity/api/v1/")
	if err != nil {
		return nil, nil
	}
	client := NewUbuntuStoreAuthService(&SnapUbuntuStoreConfig{IdentityURI: url})
	return mockServer, client
}

func (s *authTestSuite) TestRequestDeviceNonce(c *C) {
	mockServer, service := makeMockedService(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/identity/api/v1/nonces")
		io.WriteString(w, mockStoreReturnNonce)
	}))
	defer mockServer.Close()

	nonce, err := service.requestDeviceNonce()
	c.Assert(err, IsNil)
	c.Assert(nonce, DeepEquals, []byte("the-opaque-nonce"))
}

func (s *authTestSuite) TestRequestDeviceNonceError(c *C) {
	mockServer, service := makeMockedService(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer mockServer.Close()

	nonce, err := service.requestDeviceNonce()
	c.Assert(err, ErrorMatches, "cannot authenticate device to store: failed to get nonce: store server returned status 500")
	c.Assert(nonce, IsNil)
}

func (s *authTestSuite) TestRequestDeviceMacaroon(c *C) {
	mockServer, service := makeMockedService(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/identity/api/v1/sessions")
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Assert(string(jsonReq), Equals, `{"serial-assertion":"the-serial-assertion","nonce":"the-opaque-nonce","signature":"the-nonce-signature"}`)
		io.WriteString(w, mockStoreReturnMacaroon)
	}))
	defer mockServer.Close()

	macaroon, err := service.requestDeviceMacaroon([]byte("the-serial-assertion"), []byte("the-opaque-nonce"), []byte("the-nonce-signature"))
	c.Assert(err, IsNil)
	c.Assert(macaroon, Equals, "the-root-macaroon-serialized-data")
}

func (s *authTestSuite) TestRequestDeviceMacaroonError(c *C) {
	mockServer, service := makeMockedService(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer mockServer.Close()

	macaroon, err := service.requestDeviceMacaroon([]byte("the-serial-assertion"), []byte("the-opaque-nonce"), []byte("the-nonce-signature"))
	c.Assert(err, ErrorMatches, "cannot authenticate device to store: failed to get macaroon: store server returned status 500")
	c.Assert(macaroon, Equals, "")
}

func mockSignNonce(serialAssertion []byte, nonce []byte) ([]byte, error) {
	return []byte(string(nonce) + " was signed"), nil
}

const testSerial = `type: serial
authority-id: canonical
brand-id: the-brand
model: the-model
serial: the-serial
timestamp: 2016-06-11T12:00:00Z
device-key:
 openpgp xsBNBFaXv5MBCACkK//qNb3UwRtDviGcCSEi8Z6d5OXok3yilQmEh0LuW6DyP9sVpm08Vb1LGewOa5dThWGX4XKRBI/jCUnjCJQ6v15lLwHe1N7MJQ58DUxKqWFMV9yn4RcDPk6LqoFpPGdRrbp9Ivo3PqJRMyD0wuJk9RhbaGZmILcL//BLgomE9NgQdAfZbiEnGxtkqAjeVtBtcJIj5TnCC658ZCqwugQeO9iJuIn3GosYvvTB6tReq6GP6b4dqvoi7SqxHVhtt2zD4Y6FUZIVmvZK0qwkV0gua2azLzPOeoVcU1AEl7HVeBk7G6GiT5jx+CjjoGa0j22LdJB9S3JXHtGYk5p9CAwhABEBAAE=

openpgp c2ln1`

func (s *authTestSuite) TestAcquireDeviceMacaroon(c *C) {
	mockServer, service := makeMockedService(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)

		switch r.URL.Path {
		case "/identity/api/v1/nonces":
			io.WriteString(w, mockStoreReturnNonce)
		case "/identity/api/v1/sessions":
			var request struct {
				SerialAssertion string `json:"serial-assertion"`
				Nonce           string `json:"nonce"`
				Signature       string `json:"signature"`
			}
			err := json.Unmarshal(jsonReq, &request)
			c.Assert(err, IsNil)
			c.Assert(request.SerialAssertion, Equals, testSerial)
			c.Assert(request.Nonce, Equals, "the-opaque-nonce")
			c.Assert(request.Signature, Equals, "the-opaque-nonce was signed")
			io.WriteString(w, mockStoreReturnMacaroon)

		default:
			panic("unhandled path: " + r.URL.Path)
		}
	}))
	defer mockServer.Close()

	assert, err := asserts.Decode([]byte(testSerial))
	c.Assert(err, IsNil)
	serialAssertion := assert.(*asserts.Serial)

	macaroon, err := service.AcquireDeviceMacaroon(serialAssertion, mockSignNonce)
	c.Assert(err, IsNil)
	c.Assert(macaroon, Equals, "the-root-macaroon-serialized-data")
}
