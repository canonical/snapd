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
	"encoding/json"
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

type BuySnapSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&BuySnapSuite{})

type expectedURL struct {
	Body    string
	Checker func(r *http.Request)

	callCount int
}

type expectedMethod map[string]*expectedURL

type expectedMethods map[string]*expectedMethod

type buyTestMockSnapServer struct {
	ExpectedMethods expectedMethods

	Checker *check.C
}

func (s *buyTestMockSnapServer) serveHttp(w http.ResponseWriter, r *http.Request) {
	method := s.ExpectedMethods[r.Method]
	if method == nil || len(*method) == 0 {
		s.Checker.Fatalf("unexpected HTTP method %s", r.Method)
	}

	url := (*method)[r.URL.Path]
	if url == nil {
		s.Checker.Fatalf("unexpected URL %q", r.URL.Path)
	}

	if url.Checker != nil {
		url.Checker(r)
	}
	fmt.Fprintln(w, url.Body)
	url.callCount++
}

func (s *buyTestMockSnapServer) checkCounts() {
	for _, method := range s.ExpectedMethods {
		for _, url := range *method {
			s.Checker.Check(url.callCount, check.Equals, 1)
		}
	}
}

func (s *BuySnapSuite) SetUpTest(c *check.C) {
	s.BaseSnapSuite.SetUpTest(c)
	s.Login(c)
}

func (s *BuySnapSuite) TearDownTest(c *check.C) {
	s.Logout(c)
	s.BaseSnapSuite.TearDownTest(c)
}

func (s *BuySnapSuite) TestBuyHelp(c *check.C) {
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"buy"}))
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "the required argument `<snap>` was not provided")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *BuySnapSuite) TestBuyInvalidCharacters(c *check.C) {
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"buy", "a:b"}))
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "cannot buy snap: invalid characters in name")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")

	_ = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"buy", "c*d"}))
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "cannot buy snap: invalid characters in name")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

const buyFreeSnapFailsFindJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
      "developer": "canonical",
      "publisher": {
         "id": "canonical",
         "username": "canonical",
         "display-name": "Canonical",
         "validation": "verified"
      },
      "download-size": 65536,
      "icon": "",
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "name": "hello",
      "private": false,
      "resource": "/v2/snaps/hello",
      "revision": "1",
      "status": "available",
      "summary": "GNU Hello, the \"hello world\" snap",
      "type": "app",
      "version": "2.10"
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *BuySnapSuite) TestBuyFreeSnapFails(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find": &expectedURL{
					Body: buyFreeSnapFailsFindJson,
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"buy", "hello"}))
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "cannot buy snap: snap is free")
	c.Assert(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

const buySnapFindJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "channel": "stable",
      "confinement": "strict",
      "description": "GNU hello prints a friendly greeting. This is part of the snapcraft tour at https://snapcraft.io/",
      "developer": "canonical",
      "publisher": {
         "id": "canonical",
         "username": "canonical",
         "display-name": "Canonical",
         "validation": "verified"
      },
      "download-size": 65536,
      "icon": "",
      "id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
      "name": "hello",
      "private": false,
      "resource": "/v2/snaps/hello",
      "revision": "1",
      "status": "priced",
      "summary": "GNU Hello, the \"hello world\" snap",
      "type": "app",
      "version": "2.10",
      "prices": {"USD": 3.99, "GBP": 2.99}
    }
  ],
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func buySnapFindURL(c *check.C) *expectedURL {
	return &expectedURL{
		Body: buySnapFindJson,
		Checker: func(r *http.Request) {
			c.Check(r.URL.Query().Get("name"), check.Equals, "hello")
		},
	}
}

const buyReadyJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": true,
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func buyReady(c *check.C) *expectedURL {
	return &expectedURL{
		Body: buyReadyJson,
	}
}

const buySnapJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "state": "Complete"
  },
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

const loginJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "id": 1,
    "username": "username",
    "email": "hello@mail.com",
    "macaroon": "1234abcd",
    "discharges": ["a", "b", "c"]
  },
  "sources": [
    "store"
  ]
}
`

func (s *BuySnapSuite) TestBuySnapSuccess(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find":      buySnapFindURL(c),
				"/v2/buy/ready": buyReady(c),
			},
			"POST": &expectedMethod{
				"/v2/login": &expectedURL{
					Body: loginJson,
				},
				"/v2/buy": &expectedURL{
					Body: buySnapJson,
					Checker: func(r *http.Request) {
						var postData struct {
							SnapID   string  `json:"snap-id"`
							Price    float64 `json:"price"`
							Currency string  `json:"currency"`
						}
						decoder := json.NewDecoder(r.Body)
						mylog.Check(decoder.Decode(&postData))
						c.Assert(err, check.IsNil)

						c.Check(postData.SnapID, check.Equals, "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6")
						c.Check(postData.Price, check.Equals, 2.99)
						c.Check(postData.Currency, check.Equals, "GBP")
					},
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	// Confirm the purchase.
	s.password = "the password"

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"buy", "hello"}))
	c.Check(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `Please re-enter your Ubuntu One password to purchase "hello" from "canonical"
for 2.99GBP. Press ctrl-c to cancel.
Password of "hello@mail.com": 
Thanks for purchasing "hello". You may now install it on any of your devices
with 'snap install hello'.
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const buySnapPaymentDeclinedJson = `
{
  "type": "error",
  "result": {
    "message": "payment declined",
    "kind": "payment-declined"
  },
  "status-code": 400
}
`

func (s *BuySnapSuite) TestBuySnapPaymentDeclined(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find":      buySnapFindURL(c),
				"/v2/buy/ready": buyReady(c),
			},
			"POST": &expectedMethod{
				"/v2/login": &expectedURL{
					Body: loginJson,
				},
				"/v2/buy": &expectedURL{
					Body: buySnapPaymentDeclinedJson,
					Checker: func(r *http.Request) {
						var postData struct {
							SnapID   string  `json:"snap-id"`
							Price    float64 `json:"price"`
							Currency string  `json:"currency"`
						}
						decoder := json.NewDecoder(r.Body)
						mylog.Check(decoder.Decode(&postData))
						c.Assert(err, check.IsNil)

						c.Check(postData.SnapID, check.Equals, "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6")
						c.Check(postData.Price, check.Equals, 2.99)
						c.Check(postData.Currency, check.Equals, "GBP")
					},
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	// Confirm the purchase.
	s.password = "the password"

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"buy", "hello"}))
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `Sorry, your payment method has been declined by the issuer. Please review your
payment details at https://my.ubuntu.com/payment/edit and try again.`)
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, `Please re-enter your Ubuntu One password to purchase "hello" from "canonical"
for 2.99GBP. Press ctrl-c to cancel.
Password of "hello@mail.com": 
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const readyToBuyNoPaymentMethodJson = `
{
  "type": "error",
  "result": {
      "message": "no payment methods",
      "kind": "no-payment-methods"
    },
    "status-code": 400
}`

func (s *BuySnapSuite) TestBuySnapFailsNoPaymentMethod(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find": buySnapFindURL(c),
				"/v2/buy/ready": &expectedURL{
					Body: readyToBuyNoPaymentMethodJson,
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"buy", "hello"}))
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `You need to have a payment method associated with your account in order to buy a snap, please visit https://my.ubuntu.com/payment/edit to add one.

Once you’ve added your payment details, you just need to run 'snap buy hello' again.`)
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

const readyToBuyNotAcceptedTermsJson = `
{
  "type": "error",
  "result": {
      "message": "terms of service not accepted",
      "kind": "terms-not-accepted"
    },
    "status-code": 400
}`

func (s *BuySnapSuite) TestBuySnapFailsNotAcceptedTerms(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find": buySnapFindURL(c),
				"/v2/buy/ready": &expectedURL{
					Body: readyToBuyNotAcceptedTermsJson,
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"buy", "hello"}))
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `In order to buy "hello", you need to agree to the latest terms and conditions. Please visit https://my.ubuntu.com/payment/edit to do this.

Once completed, return here and run 'snap buy hello' again.`)
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *BuySnapSuite) TestBuyFailsWithoutLogin(c *check.C) {
	// We don't login here
	s.Logout(c)

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"buy", "hello"}))
	c.Check(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "You need to be logged in to purchase software. Please run 'snap login' and try again.")
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
