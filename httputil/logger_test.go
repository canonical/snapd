// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package httputil_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

func TestHTTPUtil(t *testing.T) { check.TestingT(t) }

type loggerSuite struct {
	logbuf        logger.MockedLogger
	restoreLogger func()
}

var _ = check.Suite(&loggerSuite{})

func (s *loggerSuite) TearDownTest(c *check.C) {
	s.restoreLogger()
}

func (s *loggerSuite) SetUpTest(c *check.C) {
	s.logbuf = bytes.NewBuffer(nil)
	s.logbuf, s.restoreLogger = logger.MockLogger()
}

func (loggerSuite) TestFlags(c *check.C) {
	for _, f := range []interface{}{
		httputil.DebugRequest,
		httputil.DebugResponse,
		httputil.DebugBody,
		httputil.DebugRequest | httputil.DebugResponse | httputil.DebugBody,
	} {
		os.Setenv("TEST_FOO", fmt.Sprintf("%d", f))
		tr := &httputil.LoggedTransport{
			Key: "TEST_FOO",
		}

		c.Check(httputil.GetFlags(tr), check.Equals, f)
	}
}

type fakeTransport struct {
	req *http.Request
	rsp *http.Response
}

func (t *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.req = req
	return t.rsp, nil
}

func (s loggerSuite) TestLogging(c *check.C) {
	req, err := http.NewRequest("WAT", "http://example.com/", nil)
	c.Assert(err, check.IsNil)
	rsp := &http.Response{
		Status:     "999 WAT",
		StatusCode: 999,
	}
	tr := &httputil.LoggedTransport{
		Transport: &fakeTransport{
			rsp: rsp,
		},
		Key: "TEST_FOO",
	}

	os.Setenv("TEST_FOO", "7")

	aRsp, err := tr.RoundTrip(req)
	c.Assert(err, check.IsNil)
	c.Check(aRsp, check.Equals, rsp)
	c.Check(s.logbuf.String(), check.Matches, `(?ms).*> "WAT / HTTP/\S+.*`)
	c.Check(s.logbuf.String(), check.Matches, `(?ms).*< "HTTP/\S+ 999 WAT.*`)
}

func (s loggerSuite) TestNotLoggingOctetStream(c *check.C) {
	req, err := http.NewRequest("GET", "http://example.com/data", nil)
	c.Assert(err, check.IsNil)
	needle := "lots of binary data"
	rsp := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Header: http.Header{
			"Content-Type": []string{"application/octet-stream"},
		},
		Body: io.NopCloser(strings.NewReader(needle)),
	}
	tr := &httputil.LoggedTransport{
		Transport: &fakeTransport{
			rsp: rsp,
		},
		Key: "TEST_FOO",
	}

	os.Setenv("TEST_FOO", "7")

	aRsp, err := tr.RoundTrip(req)
	c.Assert(err, check.IsNil)
	c.Check(aRsp, check.Equals, rsp)
	c.Check(s.logbuf.String(), check.Matches, `(?ms).*> "GET /data HTTP/\S+.*`)
	c.Check(s.logbuf.String(), check.Not(testutil.Contains), needle)
}

func (s loggerSuite) TestRedir(c *check.C) {
	n := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/")
			c.Check(r.Header.Get("User-Agent"), check.Equals, "fancy-agent")
			c.Check(r.Header.Get("Range"), check.Equals, "42")
			c.Check(r.Header.Get("Authorization"), check.Equals, "please")
			c.Check(r.Header.Get("Cookie"), check.Equals, "chocolate chip")
			// go1.8 changes the policy for headers on redirect:
			//   https://golang.org/pkg/net/http/#Client
			// sensitive headers are only cleaned if the redirect
			// goes to a different domain, so we simulate this
			// here
			r.URL.Host = strings.Replace(r.Host, "127.0.0.1", "localhost", -1)
			http.Redirect(w, r, r.URL.String(), 302)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/")
			c.Check(r.Header.Get("User-Agent"), check.Equals, "fancy-agent")
			c.Check(r.Header.Get("Range"), check.Equals, "42")
			c.Check(r.Header.Get("Authorization"), check.Equals, "")
			c.Check(r.Header.Get("Cookie"), check.Equals, "")
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	}))
	defer server.Close()

	client := httputil.NewHTTPClient(nil)
	req, err := http.NewRequest("GET", server.URL, nil)
	c.Assert(err, check.IsNil)
	// some headers that should be copied
	req.Header.Set("User-Agent", "fancy-agent")
	req.Header.Set("Range", "42")

	// some headers that shouldn't
	req.Header.Set("Authorization", "please")
	req.Header.Set("Cookie", "chocolate chip")

	_, err = client.Do(req)
	c.Assert(err, check.IsNil)
	c.Check(n, check.Equals, 2)
}
