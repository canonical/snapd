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

package store_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"gopkg.in/check.v1"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type userInfoSuite struct {
	testutil.BaseTest

	restoreLogger func()
}

var _ = check.Suite(&userInfoSuite{})

// obtained via:
//  `curl https://login.staging.ubuntu.com/api/v2/keys/mvo@ubuntu.com`
//  `curl https://login.staging.ubuntu.com/api/v2/keys/xDPXBdB`
var mockServerJSON = `{
    "username": "mvo",
    "ssh_keys": [
        "ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAIEAqwsTkky+laeukWyGFmtiAQUFgjD+wKYuRtOj11gjTe3qUNDgMR54W8IUELZ6NwNWs2wium+jQZLY4vlsDq4PkYK8J2qgjRZURCKp4JbjbVNSg2WO7vDtl+0FIC1GaCdglRVWffrwKN1RLlwqBCVXi01nnTk3+hEpWddjqoTXMwM= egon@top",
        "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDKBFmfD1KNULZv35907+ArIfxdGGzF1XCQj287AgK7k5GWcEdnUQfkSUHRZ4cNOqshY6W3CyDzVAmaDmeB9A7qpmsVlQp2D8y253+F2NMm1bcDdT3weG5vxkdF5qdx99gRMwDYJ4WZgIryrCAOqDLKmoSEuyuh1Zil9pDGPh/grf+EgXzDFnntgE8XJVKIldsbUplCmycSNtk47PtJATJ8q5v2dIazlxwmxKfarXS7x805u4ElrZ2h3JMCOOfL1k3sJbYc4JbZ6zB8DAhSsZ79KrStn3DE+gULmPJjM0HEbtouegZpE5wcHldoo4Oi78uNrwtv1lWp4AnK/Xwm3bl/ egon@bod\r\n"
    ],
    "openid_identifier": "xDPXBdB"
}`

func (t *userInfoSuite) SetUpTest(c *check.C) {
	t.BaseTest.SetUpTest(c)

	_, t.restoreLogger = logger.MockLogger()

	store.MockDefaultRetryStrategy(&t.BaseTest, retry.LimitCount(6, retry.LimitTime(1*time.Second,
		retry.Exponential{
			Initial: 1 * time.Millisecond,
			Factor:  1.1,
		},
	)))
}

func (t *userInfoSuite) TearDownTest(c *check.C) {
	t.BaseTest.TearDownTest(c)

	t.restoreLogger()
}

func (s *userInfoSuite) redirectToTestSSO(handler func(http.ResponseWriter, *http.Request)) {
	server := httptest.NewServer(http.HandlerFunc(handler))
	s.BaseTest.AddCleanup(func() { server.Close() })
	os.Setenv("SNAPPY_FORCE_SSO_URL", server.URL+"/api/v2")
	s.BaseTest.AddCleanup(func() { os.Unsetenv("SNAPPY_FORCE_SSO_URL") })
}

func (s *userInfoSuite) TestCreateUser(c *check.C) {
	n := 0
	s.redirectToTestSSO(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0, 1:
			w.WriteHeader(500) // force retry of the request
		case 2:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/api/v2/keys/popper@lse.ac.uk")
			fmt.Fprintln(w, mockServerJSON)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})

	info, err := store.UserInfo("popper@lse.ac.uk")
	c.Assert(err, check.IsNil)
	c.Assert(n, check.Equals, 3) // number of requests after retries
	c.Check(info.Username, check.Equals, "mvo")
	c.Check(info.OpenIDIdentifier, check.Equals, "xDPXBdB")
	c.Check(info.SSHKeys, check.DeepEquals, []string{"ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAIEAqwsTkky+laeukWyGFmtiAQUFgjD+wKYuRtOj11gjTe3qUNDgMR54W8IUELZ6NwNWs2wium+jQZLY4vlsDq4PkYK8J2qgjRZURCKp4JbjbVNSg2WO7vDtl+0FIC1GaCdglRVWffrwKN1RLlwqBCVXi01nnTk3+hEpWddjqoTXMwM= egon@top",
		"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDKBFmfD1KNULZv35907+ArIfxdGGzF1XCQj287AgK7k5GWcEdnUQfkSUHRZ4cNOqshY6W3CyDzVAmaDmeB9A7qpmsVlQp2D8y253+F2NMm1bcDdT3weG5vxkdF5qdx99gRMwDYJ4WZgIryrCAOqDLKmoSEuyuh1Zil9pDGPh/grf+EgXzDFnntgE8XJVKIldsbUplCmycSNtk47PtJATJ8q5v2dIazlxwmxKfarXS7x805u4ElrZ2h3JMCOOfL1k3sJbYc4JbZ6zB8DAhSsZ79KrStn3DE+gULmPJjM0HEbtouegZpE5wcHldoo4Oi78uNrwtv1lWp4AnK/Xwm3bl/ egon@bod\r\n"})
}

func (s *userInfoSuite) TestCreateUser500RetriesExhausted(c *check.C) {
	n := 0
	s.redirectToTestSSO(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		n++
	})

	_, err := store.UserInfo("popper@lse.ac.uk")
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, `cannot look up user.*?got unexpected HTTP status code 500.*`)
	c.Assert(n, check.Equals, 6)
}
