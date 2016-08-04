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

	snap "github.com/snapcore/snapd/cmd/snap"
)

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

func (s *SnapSuite) TestBuyHelp(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"buy"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "the required argument `<snap-name>` was not provided")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestBuyInvalidCharacters(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"buy", "a:b"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "cannot buy snap \"a:b\": invalid characters in name")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")

	_, err = snap.Parser().ParseArgs([]string{"buy", "c*d"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "cannot buy snap \"c*d\": invalid characters in name")
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

func (s *SnapSuite) TestBuyFreeSnapFails(c *check.C) {
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

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "cannot buy snap \"hello\": snap is free")
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

const buyMethodsAllowsAutomaticPaymentJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "allows-automatic-payment": true,
    "methods": [
      {
        "backend-id": "credit_card",
        "currencies": ["USD", "GBP"],
        "description": "**** **** **** 1111 (exp 23/2020)",
        "id": 123,
        "preferred": true,
        "requires-interaction": false
      }
    ]
  },
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func buyMethodsAllowsAutomaticPaymentURL(c *check.C) *expectedURL {
	return &expectedURL{
		Body: buyMethodsAllowsAutomaticPaymentJson,
	}
}

const buySnapJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "snap_id": "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6",
    "refundable_until": "2015-07-15 18:46:21",
    "state": "Complete"
  },
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestBuySnapAutomaticPayment(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find":        buySnapFindURL(c),
				"/v2/buy/methods": buyMethodsAllowsAutomaticPaymentURL(c),
			},
			"POST": &expectedMethod{
				"/v2/buy": &expectedURL{
					Body: buySnapJson,
					Checker: func(r *http.Request) {
						var postData struct {
							SnapID   string  `json:"snap-id"`
							SnapName string  `json:"snap-name"`
							Price    float64 `json:"price"`
							Currency string  `json:"currency"`
						}
						decoder := json.NewDecoder(r.Body)
						err := decoder.Decode(&postData)
						c.Assert(err, check.IsNil)

						c.Check(postData.SnapID, check.Equals, "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6")
						c.Check(postData.SnapName, check.Equals, "hello")
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
	fmt.Fprint(s.stdin, "y\n")

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Check(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "Do you want to buy \"hello\" from \"canonical\" for 2.99GBP? (Y/n): hello bought\n")
	c.Check(s.Stderr(), check.Equals, "")
}

const buyMethodsSelectPaymentMethodJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "allows-automatic-payment": false,
    "methods": [
      {
        "backend-id": "credit_card",
        "currencies": ["USD", "GBP"],
        "description": "**** **** **** 1111 (exp 23/2020)",
        "id": 123,
        "preferred": false,
        "requires-interaction": false
      },
      {
        "backend-id": "credit_card",
        "currencies": ["USD", "GBP"],
        "description": "**** **** **** 2222 (exp 23/2025)",
        "id": 234,
        "preferred": false,
        "requires-interaction": false
      },
      {
        "backend-id": "rest_paypal",
        "currencies": ["USD", "GBP", "EUR"],
        "description": "PayPal",
        "id": 345,
        "preferred": false,
        "requires-interaction": true
      }
    ]
  },
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestBuySnapSelectPaymentMethod(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find": buySnapFindURL(c),
				"/v2/buy/methods": &expectedURL{
					Body: buyMethodsSelectPaymentMethodJson,
				},
			},
			"POST": &expectedMethod{
				"/v2/buy": &expectedURL{
					Body: buySnapJson,
					Checker: func(r *http.Request) {
						var postData struct {
							SnapID    string  `json:"snap-id"`
							SnapName  string  `json:"snap-name"`
							Price     float64 `json:"price"`
							Currency  string  `json:"currency"`
							MethodID  int     `json:"method-id"`
							BackendID string  `json:"backend-id"`
						}
						decoder := json.NewDecoder(r.Body)
						err := decoder.Decode(&postData)
						c.Assert(err, check.IsNil)

						c.Check(postData.SnapID, check.Equals, "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6")
						c.Check(postData.SnapName, check.Equals, "hello")
						c.Check(postData.Price, check.Equals, 2.99)
						c.Check(postData.Currency, check.Equals, "GBP")

						// Confirm the correct details for card #2
						c.Check(postData.MethodID, check.Equals, 234)
						c.Check(postData.BackendID, check.Equals, "credit_card")
					},
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	// Select the second card
	fmt.Fprint(s.stdin, "2\n")
	// Confirm "yes" to the purchase
	fmt.Fprint(s.stdin, "\n")

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Check(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `  Selection  Description
  1          **** **** **** 1111 (exp 23/2020)
  2          **** **** **** 2222 (exp 23/2025)
  3          PayPal
Type a number to select payment method: Do you want to buy "hello" from "canonical" for 2.99GBP? (Y/n): hello bought
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const buyMethodsSelectPaymentMethodWithDefaultJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "allows-automatic-payment": false,
    "methods": [
      {
        "backend-id": "credit_card",
        "currencies": ["GBP"],
        "description": "**** **** **** 1111 (exp 23/2020)",
        "id": 123,
        "preferred": true,
        "requires-interaction": false
      },
      {
        "backend-id": "credit_card",
        "currencies": ["USD"],
        "description": "**** **** **** 2222 (exp 23/2025)",
        "id": 234,
        "preferred": false,
        "requires-interaction": false
      },
      {
        "backend-id": "rest_paypal",
        "currencies": ["USD", "GBP", "EUR"],
        "description": "PayPal",
        "id": 345,
        "preferred": false,
        "requires-interaction": true
      }
    ]
  },
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestBuySnapSelectPaymentMethodWithDefault(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find": buySnapFindURL(c),
				"/v2/buy/methods": &expectedURL{
					Body: buyMethodsSelectPaymentMethodWithDefaultJson,
				},
			},
			"POST": &expectedMethod{
				"/v2/buy": &expectedURL{
					Body: buySnapJson,
					Checker: func(r *http.Request) {
						var postData struct {
							SnapID    string  `json:"snap-id"`
							SnapName  string  `json:"snap-name"`
							Price     float64 `json:"price"`
							Currency  string  `json:"currency"`
							MethodID  int     `json:"method-id"`
							BackendID string  `json:"backend-id"`
						}
						decoder := json.NewDecoder(r.Body)
						err := decoder.Decode(&postData)
						c.Assert(err, check.IsNil)

						c.Check(postData.SnapID, check.Equals, "mVyGrEwiqSi5PugCwyH7WgpoQLemtTd6")
						c.Check(postData.SnapName, check.Equals, "hello")
						c.Check(postData.Price, check.Equals, 2.99)
						c.Check(postData.Currency, check.Equals, "GBP")

						// Confirm the correct details for card #1
						c.Check(postData.MethodID, check.Equals, 123)
						c.Check(postData.BackendID, check.Equals, "credit_card")
					},
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	// Select the default card
	fmt.Fprint(s.stdin, "\n")
	// Confirm "yes" to the purchase
	fmt.Fprint(s.stdin, "\n")

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Check(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `   Selection  Description
*  1          **** **** **** 1111 (exp 23/2020)
   2          **** **** **** 2222 (exp 23/2025)
   3          PayPal
Press <enter> to use your default[*], or type a number to select payment method: Do you want to buy "hello" from "canonical" for 2.99GBP? (Y/n): hello bought
`)
	c.Check(s.Stderr(), check.Equals, "")
}

const buyNoPaymentMethodsJson = `
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": {
    "allows-automatic-payment": false,
    "methods": []
  },
  "sources": [
    "store"
  ],
  "suggested-currency": "GBP"
}
`

func (s *SnapSuite) TestBuySnapFailsNoPaymentMethods(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find": buySnapFindURL(c),
				"/v2/buy/methods": &expectedURL{
					Body: buyNoPaymentMethodsJson,
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `cannot buy snap "hello": no payment methods registered`)
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestBuySnapFailsInvalidMethodID(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find": buySnapFindURL(c),
				"/v2/buy/methods": &expectedURL{
					Body: buyMethodsSelectPaymentMethodJson,
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	// Type an invalid number
	fmt.Fprint(s.stdin, "abc\n")

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `cannot buy snap "hello": invalid payment method selection "abc"`)
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, `  Selection  Description
  1          **** **** **** 1111 (exp 23/2020)
  2          **** **** **** 2222 (exp 23/2025)
  3          PayPal
Type a number to select payment method: `)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestBuySnapFailsEmptyMethodID(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find": buySnapFindURL(c),
				"/v2/buy/methods": &expectedURL{
					Body: buyMethodsSelectPaymentMethodJson,
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	// Type an invalid number
	fmt.Fprint(s.stdin, "\n")

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `cannot buy snap "hello": invalid payment method selection ""`)
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, `  Selection  Description
  1          **** **** **** 1111 (exp 23/2020)
  2          **** **** **** 2222 (exp 23/2025)
  3          PayPal
Type a number to select payment method: `)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestBuySnapFailsOutOfRangeMethodID(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find": buySnapFindURL(c),
				"/v2/buy/methods": &expectedURL{
					Body: buyMethodsSelectPaymentMethodJson,
				},
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	// Payment method selection out of range
	fmt.Fprint(s.stdin, "5\n")

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `cannot buy snap "hello": unknown payment method selection 5`)
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, `  Selection  Description
  1          **** **** **** 1111 (exp 23/2020)
  2          **** **** **** 2222 (exp 23/2025)
  3          PayPal
Type a number to select payment method: `)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestBuyCancel(c *check.C) {
	mockServer := &buyTestMockSnapServer{
		ExpectedMethods: expectedMethods{
			"GET": &expectedMethod{
				"/v2/find":        buySnapFindURL(c),
				"/v2/buy/methods": buyMethodsAllowsAutomaticPaymentURL(c),
			},
		},
		Checker: c,
	}
	defer mockServer.checkCounts()
	s.RedirectClientToTestServer(mockServer.serveHttp)

	// Decline the payment
	fmt.Fprint(s.stdin, "no\n")

	rest, err := snap.Parser().ParseArgs([]string{"buy", "hello"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "aborting")
	c.Check(rest, check.DeepEquals, []string{"hello"})
	c.Check(s.Stdout(), check.Equals, `Do you want to buy "hello" from "canonical" for 2.99GBP? (Y/n): `)
	c.Check(s.Stderr(), check.Equals, "")
}
