// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"net/http"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

const happyModelAssertionResponse = `type: model
authority-id: mememe
series: 16
brand-id: mememe
model: test-model
architecture: amd64
base: core18
gadget: pc=18
kernel: pc-kernel=18
required-snaps:
  - core
  - hello-world
timestamp: 2017-07-27T00:00:00.0Z
sign-key-sha3-384: 8B3Wmemeu3H6i4dEV4Q85Q4gIUCHIBCNMHq49e085QeLGHi7v27l3Cqmemer4__t

AcLBcwQAAQoAHRYhBMbX+t6MbKGH5C3nnLZW7+q0g6ELBQJdTdwTAAoJELZW7+q0g6ELEvgQAI3j
jXTqR6kKOqvw94pArwdMDUaZ++tebASAZgso8ejrW2DQGWSc0Q7SQICIR8bvHxqS1GtupQswOzwS
U8hjDTv7WEchH1jylyTj/1W1GernmitTKycecRlEkSOE+EpuqBFgTtj6PdA1Fj3CiCRi1rLMhgF2
luCOitBLaP+E8P3fuATsLqqDLYzt1VY4Y14MU75hMn+CxAQdnOZTI+NzGMasPsldmOYCPNaN/b3N
6/fDLU47RtNlMJ3K0Tz8kj0bqRbegKlD0RdNbAgo9iZwNmrr5E9WCu9f/0rUor/NIxO77H2ExIll
zhmsZ7E6qlxvAgBmzKgAXrn68gGrBkIb0eXKiCaKy/i2ApvjVZ9HkOzA6Ldd+SwNJv/iA8rdiMsq
p2BfKV5f3ju5b6+WktHxAakJ8iqQmj9Yh7piHjsOAUf1PEJd2s2nqQ+pEEn1F0B23gVCY/Fa9YRQ
iKtWVeL3rBw4dSAaK9rpTMqlNcr+yrdXfTK5YzkCC6RU4yzc5MW0hKeseeSiEDSaRYxvftjFfVNa
ZaVXKg8Lu+cHtCJDeYXEkPIDQzXswdBO1M8Mb9D0mYxQwHxwvsWv1DByB+Otq08EYgPh4kyHo7ag
85yK2e/NQ/fxSwQJMhBF74jM1z9arq6RMiE/KOleFAOraKn2hcROKnEeinABW+sOn6vNuMVv
`

const noModelAssertionYetResponse = `
{
	"type": "error",
	"status-code": 404,
	"status": "Not Found",
	"result": {
	  "message": "no model assertion yet",
	  "kind": "assertion-not-found",
	  "value": "model"
	}
}`

const noSerialAssertionYetResponse = `
{
	"type": "error",
	"status-code": 404,
	"status": "Not Found",
	"result": {
	  "message": "no serial assertion yet",
	  "kind": "assertion-not-found",
	  "value": "serial"
	}
}`

func (s *SnapSuite) TestNoModelYet(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/model")
			c.Check(r.URL.RawQuery, check.Equals, "")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			fmt.Fprintln(w, noModelAssertionYetResponse)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"model"})
	c.Assert(err, check.ErrorMatches, "device not ready - no assertion found")
}

func (s *SnapSuite) TestNoSerialYes(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/model/serial")
			c.Check(r.URL.RawQuery, check.Equals, "")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			fmt.Fprintln(w, noSerialAssertionYetResponse)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"model", "--serial"})
	c.Assert(err, check.ErrorMatches, "device not ready - no assertion found")
}

func (s *SnapSuite) TestModel(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/model")
			c.Check(r.URL.RawQuery, check.Equals, "")
			fmt.Fprintln(w, happyModelAssertionResponse)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"model"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
series:    16
brand-id:  mememe
model:     test-model
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestModelVerbose(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/model")
			c.Check(r.URL.RawQuery, check.Equals, "")
			fmt.Fprintln(w, happyModelAssertionResponse)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"model", "--verbose", "--abs-time"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
series:          16
brand-id:        mememe
model:           test-model
architecture:    amd64
base:            core18
gadget:          pc=18
kernel:          pc-kernel=18
timestamp:       2017-07-27T00:00:00Z
required-snaps:  
  - core
  - hello-world
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestModelAssertion(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/model")
			c.Check(r.URL.RawQuery, check.Equals, "")
			fmt.Fprintln(w, happyModelAssertionResponse)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"model", "--assertion"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, happyModelAssertionResponse)
	c.Check(s.Stderr(), check.Equals, "")
}
