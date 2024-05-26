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

	"github.com/ddkwork/golibrary/mylog"
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
store: mememestore
system-user-authority:
  - youyouyou
  - mememe
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

const happyUC20ModelAssertionResponse = `type: model
authority-id: testrootorg
series: 16
brand-id: testrootorg
model: test-snapd-core-20-amd64
architecture: amd64
base: core20
storage-safety: prefer-encrypted
grade: dangerous
snaps:
  -
    default-channel: 20/edge
    id: UqFziVZDHLSyO3TqSWgNBoAdHbLI4dAH
    name: pc
    type: gadget
  -
    default-channel: 20/edge
    id: pYVQrBcKmBa0mZ4CCN7ExT6jH8rY1hza
    name: pc-kernel
    type: kernel
  -
    name: app-snap
    default-channel: foo
    presence: optional
    modes:
      - recover
      - run
  -
    default-channel: latest/stable
    id: DLqre5XGLbDqg9jPtiAhRRjDuPVa5X1q
    name: core20
    type: base
  -
    default-channel: latest/stable
    id: PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4
    name: snapd
    type: snapd
timestamp: 2018-09-11T22:00:00+00:00
sign-key-sha3-384: hIedp1AvrWlcDI4uS_qjoFLzjKl5enu4G2FYJpgB3Pj-tUzGlTQBxMBsBmi-tnJR

AcLBUgQAAQoABgUCX06qogAAv10QAFaqQ0NDDvIB7LqM0xNIz+5Y6PB5wJaRk0HqVsg2LlNgS0PQ
uJf0uFMV4GjQMraL7ZYv9BGyUoA+cz8Nbiz85m1g2ADt0ugqR/x2bAojii9lbFLmWpDMJcZhrtB1
3k32lEUwqTMvzYTGiZ6TVug0KYbdmf2+5IGxsayAS3EwdrfbuGRHZOv6XGV7bmm1GEwCRAFvgHCk
BHKoLZ+rfbNclF4l6G+biWJTdyc5jCMpMQ6X/INnx2hXaMRf9Jfrpl6s2bGCfsxW6HVf7AWZ8qHK
jtWPQqJ6NFu2Kw1lYIA202ReK8DC3gfAlOeNzUG5dTPor3KwAoDJJI8ZaQypOazEhO9SHERIutbP
eqPxPmEoB2+E0/o0+g0o5jK4qww3Yd7b8FTDkqm2xfuuldWAiAA4x6ZOQb2So9OLT6ovqHnD3D2r
pLW/lhqwfKp3xzIVUrLi0sjGOVXu5xFDDRyFICZ6kwC7JynRGfHoa5E2y7rv8ehnOZQJ+esz9sgY
lCJcyJ8vhabDlVHg0msSeNKMVBwhQnOSakEwlcfVyaSnapArkF+OCAMl8cuGpMTKO7vJLIJo2c2P
jcVE0ftsTGs9eBi2HmdDhu3e3fmxHt9VcC4uRSOnYNVcJnMh0yVmG8RGS/Dqcz04II7llww6JJYG
KKjQ3RU/TduXa8VJsoWiRRUYAv3H
`

const happyModelWithDisplayNameAssertionResponse = `type: model
authority-id: mememe
series: 16
brand-id: mememe
model: test-model
architecture: amd64
display-name: Model Name
base: core18
gadget: pc=18
kernel: pc-kernel=18
store: mememestore
system-user-authority:
  - youyouyou
  - mememe
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

const happyAccountAssertionResponse = `type: account
authority-id: canonical
account-id: mememe
display-name: MeMeMe
timestamp: 2016-04-01T00:00:00.0Z
username: meuser
validation: certified
sign-key-sha3-384: -CvQKAwRQ5h3Ffn10FILJoEZUXOv6km9FwA80-Rcj-f-6jadQ89VRswHNiEB9Lxk

AcLDXAQAAQoABgUCV7UYzwAKCRDUpVvql9g3IK7uH/4udqNOurx5WYVknzXdwekp0ovHCQJ0iBPw
TSFxEVr9faZSzb7eqJ1WicHsShf97PYS3ClRYAiluFsjRA8Y03kkSVJHjC+sIwGFubsnkmgflt6D
WEmYIl0UBmeaEDS8uY4Xvp9NsLTzNEj2kvzy/52gKaTc1ZSl5RDL9ppMav+0V9iBYpiDPBWH2rJ+
aDSD8Rkyygm0UscfAKyDKH4lrvZ0WkYyi1YVNPrjQ/AtBySh6Q4iJ3LifzKa9woIyAuJET/4/FPY
oirqHAfuvNod36yNQIyNqEc20AvTvZNH0PSsg4rq3DLjIPzv5KbJO9lhsasNJK1OdL6x8Yqrdsbk
ldZp4qkzfjV7VOMQKaadfcZPRaVVeJWOBnBiaukzkhoNlQi1sdCdkBB/AJHZF8QXw6c7vPDcfnCV
1lW7ddQ2p8IsJbT6LzpJu3GW/P4xhNgCjtCJ1AJm9a9RqLwQYgdLZwwDa9iCRtqTbRXBlfy3apps
1VjbQ3h5iCd0hNfwDBnGVm1rhLKHCD1DUdNE43oN2ZlE7XGyh0HFV6vKlpqoW3eoXCIxWu+HBY96
+LSl/jQgCkb0nxYyzEYK4Reb31D0mYw1Nji5W+MIF5E09+DYZoOT0UvR05YMwMEOeSdI/hLWg/5P
k+GDK+/KopMmpd4D1+jjtF7ZvqDpmAV98jJGB2F88RyVb4gcjmFFyTi4Kv6vzz/oLpbm0qrizC0W
HLGDN/ymGA5sHzEgEx7U540vz/q9VX60FKqL2YZr/DcyY9GKX5kCG4sNqIIHbcJneZ4frM99oVDu
7Jv+DIx/Di6D1ULXol2XjxbbJLKHFtHksR97ceaFvcZwTogC61IYUBJCvvMoqdXAWMhEXCr0QfQ5
Xbi31XW2d4/lF/zWlAkRnGTzufIXFni7+nEuOK0SQEzO3/WaRedK1SGOOtTDjB8/3OJeW96AUYK5
oTIynkYkEyHWMNCXALg+WQW6L4/YO7aUjZ97zOWIugd7Xy63aT3r/EHafqaY2nacOhLfkeKZ830b
o/ezjoZQAxbh6ce7JnXRgE9ELxjdAhBTpGjmmmN2sYrJ7zP9bOgly0BnEPXGSQfFA+NNNw1FADx1
MUY8q9DBjmVtgqY+1KGTV5X8KvQCBMODZIf/XJPHdCRAHxMd8COypcwgL2vDIIXpOFbi1J/B0GF+
eklxk9wzBA8AecBMCwCzIRHDNpD1oa2we38bVFrOug6e/VId1k1jYFJjiLyLCDmV8IMYwEllHSXp
LQAdm3xZ7t4WnxYC8YSCk9mXf3CZg59SpmnV5Q5Z6A5Pl7Nc3sj7hcsMBZEsOMPzNC9dPsBnZvjs
WpPUffJzEdhHBFhvYMuD4Vqj6ejUv9l3oTrjQWVC`

// note: this serial assertion was generated by adding print statements to the
// test in api_model_test.go that generate a fake serial assertion
const happySerialAssertionResponse = `type: serial
authority-id: my-brand
brand-id: my-brand
model: my-old-model
serial: serialserial
device-key:
    AcZrBFaFwYABAvCgEOrrLA6FKcreHxCcOoTgBUZ+IRG7Nb8tzmEAklaQPGpv7skapUjwD1luE2go
    mTcoTssVHrfLpBoSDV1aBs44rg3NK40ZKPJP7d2zkds1GxUo1Ea5vfet3SJ4h3aRABEBAAE=
device-key-sha3-384: iqLo9doLzK8De9925UrdUyuvPbBad72OTWVE9YJXqd6nz9dKvwJ_lHP5bVxrl3VO
timestamp: 2019-08-26T16:34:21-05:00
sign-key-sha3-384: anCEGC2NYq7DzDEi6y7OafQCVeVLS90XlLt9PNjrRl9sim5rmRHDDNFNO7ODcWQW

AcJwBAABCgAGBQJdZFBdAADCLALwR6Sy24wm9PffwbvUhOEXneyY3BnxKC0+NgdHu1gU8go9vEP1
i+Flh5uoS70+MBIO+nmF8T+9JWIx2QWFDDxvcuFosnIhvUajCEQohauys5FMz/H/WvB0vrbTBpvK
eg==
`

const happySerialUC20AssertionResponse = `type: serial
authority-id: testrootorg
brand-id: testrootorg
model: test-snapd-core-20-amd64
serial: 7777
device-key:
    AcbBTQRWhcGAARAAuKf9n7WvZDI7u3NzMkD8WN+dxCYrb0UE9XIaHcbrj0i2zJpxCtUtpzoEo7Uk
    Cvxuhr2uBpzAa8fScwzOd77MGHIZQDpS7sFSkhYsSSN0m4sy8vRevsj0roN31fugCjRnhtLTkgxo
    KSoAsK87vYnC+m5V5AHaRER7q1KgpUoVD7eLOJZyrd/tWecsLL9OK87yAQHdF/cVlQupOP6OU3fK
    DllER6V2TD4jADK2Gyj2lDhy3F0+rE0a+zsGpmQQBorvzbozUHgBE3z/XjTTMrHYP4m+4V5HeWdn
    rHt/x1LZ8wMTCMT1eeruclC82UPRgF0zWI+P7WgBqogJpCbfadhAj1zvKW+5vJ385n0BU7PoAZtA
    KddBbsmEnfK/gWIxgFemIrYcYGhIBxYY6iNcygTYRFo4R9xm3bELHLG+viHggih4Lrjnb4sLHOdC
    h3C4/45bY+6hSno8GQGlp4kYQQM8mrF9st51jIM6oyB84NtoySLYYE1wMeGNzDHSuI+1IiRmaTgy
    Q2ImXTuqOhclhNA1sOi3R4H+oOBxe6GmoM5ATBPBqJeqUEvK8GpSRCig0QH4qMNF/abNKwvKhGMZ
    LqtpFp5LNx7xYuAwoVkcq0nxQTsXctl3gJqY+lRx7mIeoXLZPKZyJees+5v96oa9lMdNX3f5UUpX
    zq0cNhdgHrXZfcsAEQEAAQ==
device-key-sha3-384: CZeO_5nJm_Rg0izosNfcQRoQj9nFtAmK2Y_tz4YjlKlvS93b_9gTDHuby5HHwi7d
timestamp: 2020-09-03T14:42:47-05:00
sign-key-sha3-384: hIedp1AvrWlcDI4uS_qjoFLzjKl5enu4G2FYJpgB3Pj-tUzGlTQBxMBsBmi-tnJR

AcLBUgQAAQoABgUCX1FHNwAAqFoQABFiyzipoTYAuYN0Wd7cXuPPD7z+z+E+LoZZ+j4vUKqvnGX8
tksb2nEEOQhjSvVof5pPOswWgq8Nj52dtYA20R5Zgfy0MZHHcCCfgxaRj6EiFyrG5h9l5wWMnzdb
pXo9SJ3hxw6lKdj3n9RAAY0mACvw6f/trcyLeSxQ7EBm6X9c4ohJSjlHkKj0TlKkNTrFflko5aQH
uJUk/YgsvMTZUHbgj6QKHlODUH8iRvOHxzn/Y9BlnzBsb/SyzvNTPeQyzFtd9QkESI2sWghviys2
fGeEZPeXU6xts6Ht+xhr3mj5npZwkkL/6YxSzm9owQ0zGrfaFTswN+xoDKZ5498qRtSY3mCK/5xx
kvWpOTHHhfvuS3GGyvRZOih7IAffDEwQsUNh8V9IjQNNTIkCYTPZz4WBM42mI8UgeDsnDImmcoc0
GlqBeCxUigszJlEdUAHQklwW7Sgp13mceR3zB7BHgp4Sk7n0RyPuTQUA94ys6SeesK5YphwmhVed
V02lkdeqRbGt3yZ/T5Zg8CIUIM0RKDSqoHgvoCMZh98dRGv6LPRj/P0RSWmjYWotjdK+lXK1fySM
RXMNJIInZoC0x8qEwGLXVl5V3z8motLG71ie7PQ677W0dE9XM5LRnZHEKXP41jfaOO9vu12TtBsh
pe/pnYDfIzU6OyOsdmkGWaWD+nbD
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

// helper for constructing different types of responses to the client
type checkResponder func(c *check.C, w http.ResponseWriter, r *http.Request)

func simpleHappyResponder(body string) checkResponder {
	return func(c *check.C, w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "GET")
		c.Check(r.URL.RawQuery, check.Equals, "")
		fmt.Fprintln(w, body)
	}
}

func simpleUnhappyResponder(errBody string) checkResponder {
	return func(c *check.C, w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "GET")
		c.Check(r.URL.RawQuery, check.Equals, "")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		fmt.Fprintln(w, errBody)
	}
}

func simpleAssertionAccountResponder(body string) checkResponder {
	return func(c *check.C, w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "GET")
		w.Header().Set("X-Ubuntu-Assertions-Count", "1")
		fmt.Fprintln(w, body)
	}
}

func makeHappyTestServerHandler(c *check.C, modelResp, serialResp, accountResp checkResponder) func(w http.ResponseWriter, r *http.Request) {
	var nModelSerial, nModel, nKnown int
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/model":
			switch nModel {
			case 0:
				modelResp(c, w, r)
			default:
				c.Fatalf("expected to get 1 request for /v2/model, now on %d", nModel+1)
			}
			nModel++
		case "/v2/model/serial":
			switch nModelSerial {
			case 0:
				serialResp(c, w, r)
			default:
				c.Fatalf("expected to get 1 request for /v2/model, now on %d", nModelSerial+1)
			}
			nModelSerial++
		case "/v2/assertions/account":
			switch nKnown {
			case 0:
				accountResp(c, w, r)
			default:
				c.Fatalf("expected to get 1 request for /v2/model, now on %d", nKnown+1)
			}
			nKnown++
		default:
			c.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}
}

func (s *SnapSuite) TestNoModelYet(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleUnhappyResponder(noModelAssertionYetResponse),
			simpleUnhappyResponder(noSerialAssertionYetResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model"}))
	c.Assert(err, check.ErrorMatches, `device not ready yet \(no assertions found\)`)
}

func (s *SnapSuite) TestNoSerialYet(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyModelAssertionResponse),
			simpleUnhappyResponder(noSerialAssertionYetResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--serial"}))
	c.Assert(err, check.ErrorMatches, `device not registered yet \(no serial assertion found\)`)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, `
brand-id:  mememe
model:     test-model
`[1:])
}

func (s *SnapSuite) TestModel(c *check.C) {
	for _, tt := range []struct {
		comment string
		modelF  checkResponder
		serialF checkResponder
		outText string
	}{
		{
			comment: "normal serial and model asserts",
			modelF:  simpleHappyResponder(happyModelAssertionResponse),
			serialF: simpleHappyResponder(happySerialAssertionResponse),
			outText: `
brand   MeMeMe (meuser**)
model   test-model
serial  serialserial
`[1:],
		},
		{
			comment: "normal uc20 serial and model asserts",
			modelF:  simpleHappyResponder(happyUC20ModelAssertionResponse),
			serialF: simpleHappyResponder(happySerialUC20AssertionResponse),
			outText: `
brand           MeMeMe (meuser**)
model           test-snapd-core-20-amd64
grade           dangerous
storage-safety  prefer-encrypted
serial          7777
`[1:],
		},
		{
			comment: "model assert has display-name",
			modelF:  simpleHappyResponder(happyModelWithDisplayNameAssertionResponse),
			serialF: simpleHappyResponder(happySerialAssertionResponse),
			outText: `
brand   MeMeMe (meuser**)
model   Model Name (test-model)
serial  serialserial
`[1:],
		},
		{
			comment: "missing serial assert",
			modelF:  simpleHappyResponder(happyModelAssertionResponse),
			serialF: simpleUnhappyResponder(noSerialAssertionYetResponse),
			outText: `
brand   MeMeMe (meuser**)
model   test-model
serial  - (device not registered yet)
`[1:],
		},
	} {
		s.RedirectClientToTestServer(
			makeHappyTestServerHandler(
				c,
				tt.modelF,
				tt.serialF,
				simpleAssertionAccountResponder(happyAccountAssertionResponse),
			))
		rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model"}))
		c.Assert(err, check.IsNil)
		c.Assert(rest, check.DeepEquals, []string{})
		c.Check(s.Stdout(), check.Equals, tt.outText, check.Commentf("\n%s\n", tt.outText))
		c.Check(s.Stderr(), check.Equals, "")
		s.ResetStdStreams()
	}
}

func (s *SnapSuite) TestModelVerbose(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyModelAssertionResponse),
			simpleHappyResponder(happySerialAssertionResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--verbose", "--abs-time"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
brand-id:               mememe
model:                  test-model
serial:                 serialserial
architecture:           amd64
base:                   core18
gadget:                 pc=18
kernel:                 pc-kernel=18
store:                  mememestore
system-user-authority:  
  - youyouyou
  - mememe
timestamp:       2017-07-27T00:00:00Z
required-snaps:  
  - core
  - hello-world
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestModelVerboseUC20(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyUC20ModelAssertionResponse),
			simpleHappyResponder(happySerialAssertionResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--verbose", "--abs-time"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
brand-id:        testrootorg
model:           test-snapd-core-20-amd64
grade:           dangerous
storage-safety:  prefer-encrypted
serial:          serialserial
architecture:    amd64
base:            core20
timestamp:       2018-09-11T22:00:00Z
snaps:
  - name:             pc
    id:               UqFziVZDHLSyO3TqSWgNBoAdHbLI4dAH
    type:             gadget
    default-channel:  20/edge
  - name:             pc-kernel
    id:               pYVQrBcKmBa0mZ4CCN7ExT6jH8rY1hza
    type:             kernel
    default-channel:  20/edge
  - name:             app-snap
    default-channel:  foo
    presence:         optional
    modes:            [recover, run]
  - name:             core20
    id:               DLqre5XGLbDqg9jPtiAhRRjDuPVa5X1q
    type:             base
    default-channel:  latest/stable
  - name:             snapd
    id:               PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4
    type:             snapd
    default-channel:  latest/stable
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestModelVerboseDisplayName(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyModelWithDisplayNameAssertionResponse),
			simpleHappyResponder(happySerialAssertionResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--verbose", "--abs-time"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
brand-id:               mememe
model:                  test-model
serial:                 serialserial
architecture:           amd64
base:                   core18
display-name:           Model Name
gadget:                 pc=18
kernel:                 pc-kernel=18
store:                  mememestore
system-user-authority:  
  - youyouyou
  - mememe
timestamp:       2017-07-27T00:00:00Z
required-snaps:  
  - core
  - hello-world
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestModelVerboseNoSerialYet(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyModelAssertionResponse),
			simpleUnhappyResponder(noSerialAssertionYetResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--verbose", "--abs-time"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
brand-id:               mememe
model:                  test-model
serial:                 -- (device not registered yet)
architecture:           amd64
base:                   core18
gadget:                 pc=18
kernel:                 pc-kernel=18
store:                  mememestore
system-user-authority:  
  - youyouyou
  - mememe
timestamp:       2017-07-27T00:00:00Z
required-snaps:  
  - core
  - hello-world
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestModelAssertion(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyModelAssertionResponse),
			simpleHappyResponder(happySerialAssertionResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--assertion"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, happyModelAssertionResponse)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestModelAssertionVerbose(c *check.C) {
	// check that no calls to the server happen
	s.RedirectClientToTestServer(
		func(w http.ResponseWriter, r *http.Request) {
			c.Fatalf("unexpected request to %s", r.URL.Path)
		},
	)
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--assertion", "--verbose"}))
	c.Assert(err, check.ErrorMatches, "cannot use --verbose with --assertion")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestSerial(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyModelAssertionResponse),
			simpleHappyResponder(happySerialAssertionResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--serial"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
brand-id:  my-brand
model:     my-old-model
serial:    serialserial
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestSerialVerbose(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyModelAssertionResponse),
			simpleHappyResponder(happySerialAssertionResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--serial", "--verbose", "--abs-time"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `
brand-id:   my-brand
model:      my-old-model
serial:     serialserial
timestamp:  2019-08-26T16:34:21-05:00
device-key-sha3-384: |
  iqLo9doLzK8De9925UrdUyuvPbBad72OTWVE9YJXqd6nz9dKvwJ_lHP5bVxrl3VO
device-key: |
  AcZrBFaFwYABAvCgEOrrLA6FKcreHxCcOoTgBUZ+IRG7Nb8tzmEAklaQPGpv7skapUjwD1luE2g
  omTcoTssVHrfLpBoSDV1aBs44rg3NK40ZKPJP7d2zkds1GxUo1Ea5vfet3SJ4h3aRABEBAAE=
`[1:])
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestSerialAssertion(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyModelAssertionResponse),
			simpleHappyResponder(happySerialAssertionResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--serial", "--assertion"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, happySerialAssertionResponse)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestSerialAssertionSerialAssertionMissing(c *check.C) {
	s.RedirectClientToTestServer(
		makeHappyTestServerHandler(
			c,
			simpleHappyResponder(happyModelAssertionResponse),
			simpleUnhappyResponder(noSerialAssertionYetResponse),
			simpleAssertionAccountResponder(happyAccountAssertionResponse),
		))
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"model", "--serial", "--assertion"}))
	c.Assert(err, check.ErrorMatches, `device not ready yet \(no assertions found\)`)
	c.Assert(s.Stdout(), check.Equals, "")
	c.Assert(s.Stderr(), check.Equals, "")
}
