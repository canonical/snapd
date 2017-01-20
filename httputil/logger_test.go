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
	"io/ioutil"
	"net/http"
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
	logbuf *bytes.Buffer
}

var _ = check.Suite(&loggerSuite{})

func (loggerSuite) TearDownTest(c *check.C) {
	os.Unsetenv("SNAPD_DEBUG")
}

func (s *loggerSuite) SetUpTest(c *check.C) {
	os.Setenv("SNAPD_DEBUG", "true")
	s.logbuf = bytes.NewBuffer(nil)
	l, err := logger.NewConsoleLog(s.logbuf, logger.DefaultFlags)
	c.Assert(err, check.IsNil)
	logger.SetLogger(l)
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
		Body: ioutil.NopCloser(strings.NewReader(needle)),
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
