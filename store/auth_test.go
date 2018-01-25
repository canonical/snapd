// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/testutil"
)

type authTestSuite struct {
	testutil.BaseTest
}

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

const mockStoreReturnMacaroon = `{"macaroon": "the-root-macaroon-serialized-data"}`

const mockStoreReturnDischarge = `{"discharge_macaroon": "the-discharge-macaroon-serialized-data"}`

const mockStoreReturnNoMacaroon = `{}`

const mockStoreReturnNonce = `{"nonce": "the-nonce"}`

const mockStoreReturnNoNonce = `{}`

func (s *authTestSuite) SetUpTest(c *C) {
	MockDefaultRetryStrategy(&s.BaseTest, retry.LimitCount(5, retry.LimitTime(1*time.Second,
		retry.Exponential{
			Initial: 1 * time.Millisecond,
			Factor:  1.1,
		},
	)))
}

func (s *authTestSuite) TestRequestStoreMacaroon(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnMacaroon)
	}))
	defer mockServer.Close()
	MacaroonACLAPI = mockServer.URL + "/acl/"

	macaroon, err := requestStoreMacaroon()
	c.Assert(err, IsNil)
	c.Assert(macaroon, Equals, "the-root-macaroon-serialized-data")
}

func (s *authTestSuite) TestRequestStoreMacaroonMissingData(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoMacaroon)
	}))
	defer mockServer.Close()
	MacaroonACLAPI = mockServer.URL + "/acl/"

	macaroon, err := requestStoreMacaroon()
	c.Assert(err, ErrorMatches, "cannot get snap access permission from store: empty macaroon returned")
	c.Assert(macaroon, Equals, "")
}

func (s *authTestSuite) TestRequestStoreMacaroonError(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		n++
	}))
	defer mockServer.Close()
	MacaroonACLAPI = mockServer.URL + "/acl/"

	macaroon, err := requestStoreMacaroon()
	c.Assert(err, ErrorMatches, "cannot get snap access permission from store: store server returned status 500")
	c.Assert(n, Equals, 5)
	c.Assert(macaroon, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveat(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnDischarge)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := dischargeAuthCaveat("third-party-caveat", "guy@example.com", "passwd", "")
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

	discharge, err := dischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
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

	discharge, err := dischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
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

	discharge, err := dischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
	c.Assert(err, Equals, ErrInvalidCredentials)
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveatMissingData(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoMacaroon)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := dischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
	c.Assert(err, ErrorMatches, "cannot authenticate to snap store: empty macaroon returned")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestDischargeAuthCaveatError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer mockServer.Close()
	UbuntuoneDischargeAPI = mockServer.URL + "/tokens/discharge"

	discharge, err := dischargeAuthCaveat("third-party-caveat", "foo@example.com", "passwd", "")
	c.Assert(err, ErrorMatches, "cannot authenticate to snap store: server returned status 500")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestRefreshDischargeMacaroon(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnDischarge)
	}))
	defer mockServer.Close()
	UbuntuoneRefreshDischargeAPI = mockServer.URL + "/tokens/refresh"

	discharge, err := refreshDischargeMacaroon("soft-expired-serialized-discharge-macaroon")
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

	discharge, err := refreshDischargeMacaroon("soft-expired-serialized-discharge-macaroon")
	c.Assert(err, Equals, ErrInvalidCredentials)
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestRefreshDischargeMacaroonMissingData(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoMacaroon)
	}))
	defer mockServer.Close()
	UbuntuoneRefreshDischargeAPI = mockServer.URL + "/tokens/refresh"

	discharge, err := refreshDischargeMacaroon("soft-expired-serialized-discharge-macaroon")
	c.Assert(err, ErrorMatches, "cannot authenticate to snap store: empty macaroon returned")
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestRefreshDischargeMacaroonError(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Assert(data, NotNil)
		c.Assert(string(data), Equals, `{"discharge_macaroon":"soft-expired-serialized-discharge-macaroon"}`)
		w.WriteHeader(500)
		n++
	}))
	defer mockServer.Close()
	UbuntuoneRefreshDischargeAPI = mockServer.URL + "/tokens/refresh"

	discharge, err := refreshDischargeMacaroon("soft-expired-serialized-discharge-macaroon")
	c.Assert(err, ErrorMatches, "cannot authenticate to snap store: server returned status 500")
	c.Assert(n, Equals, 5)
	c.Assert(discharge, Equals, "")
}

func (s *authTestSuite) TestLoginCaveatIDReturnCaveatID(c *C) {
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	c.Check(err, IsNil)
	err = m.AddThirdPartyCaveat([]byte("shared-key"), "third-party-caveat", UbuntuoneLocation)
	c.Check(err, IsNil)

	caveat, err := loginCaveatID(m)
	c.Check(err, IsNil)
	c.Check(caveat, Equals, "third-party-caveat")
}

func (s *authTestSuite) TestLoginCaveatIDMacaroonMissingCaveat(c *C) {
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	c.Check(err, IsNil)
	err = m.AddThirdPartyCaveat([]byte("shared-key"), "third-party-caveat", "other-location")
	c.Check(err, IsNil)

	caveat, err := loginCaveatID(m)
	c.Check(err, NotNil)
	c.Check(caveat, Equals, "")
}

func (s *authTestSuite) TestRequestStoreDeviceNonce(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNonce)
	}))
	defer mockServer.Close()

	deviceNonceAPI := mockServer.URL + "/api/v1/snaps/auth/nonces"
	nonce, err := requestStoreDeviceNonce(deviceNonceAPI)
	c.Assert(err, IsNil)
	c.Assert(nonce, Equals, "the-nonce")
}

func (s *authTestSuite) TestRequestStoreDeviceNonceRetry500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 4 {
			w.WriteHeader(500)
		} else {
			io.WriteString(w, mockStoreReturnNonce)
		}
	}))
	defer mockServer.Close()

	deviceNonceAPI := mockServer.URL + "/api/v1/snaps/auth/nonces"
	nonce, err := requestStoreDeviceNonce(deviceNonceAPI)
	c.Assert(err, IsNil)
	c.Assert(nonce, Equals, "the-nonce")
	c.Assert(n, Equals, 4)
}

func (s *authTestSuite) TestRequestStoreDeviceNonce500(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(500)
	}))
	defer mockServer.Close()

	deviceNonceAPI := mockServer.URL + "/api/v1/snaps/auth/nonces"
	_, err := requestStoreDeviceNonce(deviceNonceAPI)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot get nonce from store: store server returned status 500`)
	c.Assert(n, Equals, 5)
}

func (s *authTestSuite) TestRequestStoreDeviceNonceFailureOnDNS(c *C) {
	deviceNonceAPI := "http://nonexistingserver121321.com/api/v1/snaps/auth/nonces"
	_, err := requestStoreDeviceNonce(deviceNonceAPI)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot get nonce from store.*`)
}

func (s *authTestSuite) TestRequestStoreDeviceNonceEmptyResponse(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoNonce)
	}))
	defer mockServer.Close()

	deviceNonceAPI := mockServer.URL + "/api/v1/snaps/auth/nonces"
	nonce, err := requestStoreDeviceNonce(deviceNonceAPI)
	c.Assert(err, ErrorMatches, "cannot get nonce from store: empty nonce returned")
	c.Assert(nonce, Equals, "")
}

func (s *authTestSuite) TestRequestStoreDeviceNonceError(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		n++
	}))
	defer mockServer.Close()

	deviceNonceAPI := mockServer.URL + "/api/v1/snaps/auth/nonces"
	nonce, err := requestStoreDeviceNonce(deviceNonceAPI)
	c.Assert(err, ErrorMatches, "cannot get nonce from store: store server returned status 500")
	c.Assert(n, Equals, 5)
	c.Assert(nonce, Equals, "")
}

type testDeviceSessionRequestParamsEncoder struct{}

func (pe *testDeviceSessionRequestParamsEncoder) EncodedRequest() string {
	return "session-request"
}

func (pe *testDeviceSessionRequestParamsEncoder) EncodedSerial() string {
	return "serial-assertion"
}

func (pe *testDeviceSessionRequestParamsEncoder) EncodedModel() string {
	return "model-assertion"
}

func (s *authTestSuite) TestRequestDeviceSession(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Check(string(jsonReq), Equals, `{"device-session-request":"session-request","model-assertion":"model-assertion","serial-assertion":"serial-assertion"}`)
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, "")

		io.WriteString(w, mockStoreReturnMacaroon)
	}))
	defer mockServer.Close()

	deviceSessionAPI := mockServer.URL + "/api/v1/snaps/auth/sessions"
	macaroon, err := requestDeviceSession(deviceSessionAPI, &testDeviceSessionRequestParamsEncoder{}, "")
	c.Assert(err, IsNil)
	c.Assert(macaroon, Equals, "the-root-macaroon-serialized-data")
}

func (s *authTestSuite) TestRequestDeviceSessionWithPreviousSession(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Check(string(jsonReq), Equals, `{"device-session-request":"session-request","model-assertion":"model-assertion","serial-assertion":"serial-assertion"}`)
		c.Check(r.Header.Get("X-Device-Authorization"), Equals, `Macaroon root="previous-session"`)

		io.WriteString(w, mockStoreReturnMacaroon)
	}))
	defer mockServer.Close()

	deviceSessionAPI := mockServer.URL + "/api/v1/snaps/auth/sessions"
	macaroon, err := requestDeviceSession(deviceSessionAPI, &testDeviceSessionRequestParamsEncoder{}, "previous-session")
	c.Assert(err, IsNil)
	c.Assert(macaroon, Equals, "the-root-macaroon-serialized-data")
}

func (s *authTestSuite) TestRequestDeviceSessionMissingData(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnNoMacaroon)
	}))
	defer mockServer.Close()

	deviceSessionAPI := mockServer.URL + "/api/v1/snaps/auth/sessions"
	macaroon, err := requestDeviceSession(deviceSessionAPI, &testDeviceSessionRequestParamsEncoder{}, "")
	c.Assert(err, ErrorMatches, "cannot get device session from store: empty session returned")
	c.Assert(macaroon, Equals, "")
}

func (s *authTestSuite) TestRequestDeviceSessionError(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error body"))
		n++
	}))
	defer mockServer.Close()

	deviceSessionAPI := mockServer.URL + "/api/v1/snaps/auth/sessions"
	macaroon, err := requestDeviceSession(deviceSessionAPI, &testDeviceSessionRequestParamsEncoder{}, "")
	c.Assert(err, ErrorMatches, `cannot get device session from store: store server returned status 500 and body "error body"`)
	c.Assert(n, Equals, 5)
	c.Assert(macaroon, Equals, "")
}

func (s *authTestSuite) TestStringish(c *C) {
	var x stringList

	c.Check(json.Unmarshal([]byte(`"hello"`), &x), IsNil)
	c.Check(x, DeepEquals, stringList([]string{"hello"}))

	c.Check(json.Unmarshal([]byte(`["hello", "world"]`), &x), IsNil)
	c.Check(x, DeepEquals, stringList([]string{"hello", "world"}))
}
