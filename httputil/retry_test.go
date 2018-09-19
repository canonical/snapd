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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/httputil"
)

type retrySuite struct{}

var _ = Suite(&retrySuite{})

func (s *retrySuite) SetUpTest(c *C) {
}

func (s *retrySuite) TearDownTest(c *C) {
}

var testRetryStrategy = retry.LimitCount(5, retry.LimitTime(1*time.Second,
	retry.Exponential{
		Initial: 1 * time.Millisecond,
		Factor:  1,
	},
))

func (s *retrySuite) TestRetryRequestOnEOF(c *C) {
	n := 0
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 4 {
			io.WriteString(w, "{")
			mockServer.CloseClientConnections()
			return
		}
		io.WriteString(w, `{"ok": true}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	cli := httputil.NewHTTPClient(nil)

	doRequest := func() (*http.Response, error) {
		return cli.Get(mockServer.URL)
	}

	failure := false
	var got interface{}
	readResponseBody := func(resp *http.Response) error {
		failure = false
		if resp.StatusCode != 200 {
			failure = true
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(&got)
	}

	_, err := httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, IsNil)

	c.Assert(failure, Equals, false)
	c.Check(got, DeepEquals, map[string]interface{}{"ok": true})
	c.Assert(n, Equals, 4)
}

func (s *retrySuite) TestRetryRequestFailWithEOF(c *C) {
	n := 0
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		io.WriteString(w, "{")
		mockServer.CloseClientConnections()
		return
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	cli := httputil.NewHTTPClient(nil)

	doRequest := func() (*http.Response, error) {
		return cli.Get(mockServer.URL)
	}

	failure := false
	var got interface{}
	readResponseBody := func(resp *http.Response) error {
		failure = false
		if resp.StatusCode != 200 {
			failure = true
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(&got)
	}

	_, err := httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `^Get http://127.0.0.1:.*?: EOF$`)

	c.Check(failure, Equals, false)
	c.Assert(n, Equals, 5)
}

func (s *retrySuite) TestRetryRequestOn500(c *C) {
	n := 0
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 4 {
			w.WriteHeader(500)
			return
		}
		io.WriteString(w, `{"ok": true}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	cli := httputil.NewHTTPClient(nil)

	doRequest := func() (*http.Response, error) {
		return cli.Get(mockServer.URL)
	}

	failure := false
	var got interface{}
	readResponseBody := func(resp *http.Response) error {
		failure = false
		if resp.StatusCode != 200 {
			failure = true
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(&got)
	}

	_, err := httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, IsNil)

	c.Assert(failure, Equals, false)
	c.Check(got, DeepEquals, map[string]interface{}{"ok": true})
	c.Assert(n, Equals, 4)
}

func (s *retrySuite) TestRetryRequestFailOn500(c *C) {
	n := 0
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(500)
		return
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	cli := httputil.NewHTTPClient(nil)

	doRequest := func() (*http.Response, error) {
		return cli.Get(mockServer.URL)
	}

	failure := false
	var got interface{}
	readResponseBody := func(resp *http.Response) error {
		failure = false
		if resp.StatusCode != 200 {
			failure = true
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(&got)
	}

	resp, err := httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, IsNil)
	c.Assert(resp.StatusCode, Equals, 500)

	c.Check(failure, Equals, true)
	c.Assert(n, Equals, 5)
}

func (s *retrySuite) TestRetryRequestUnexpectedEOFHandling(c *C) {
	permanentlyBrokenSrvCalls := 0
	somewhatBrokenSrvCalls := 0

	mockPermanentlyBrokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permanentlyBrokenSrvCalls++
		w.Header().Add("Content-Length", "1000")
	}))
	c.Assert(mockPermanentlyBrokenServer, NotNil)
	defer mockPermanentlyBrokenServer.Close()

	mockSomewhatBrokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		somewhatBrokenSrvCalls++
		if somewhatBrokenSrvCalls > 3 {
			io.WriteString(w, `{"ok": true}`)
			return
		}
		w.Header().Add("Content-Length", "1000")
	}))
	c.Assert(mockSomewhatBrokenServer, NotNil)
	defer mockSomewhatBrokenServer.Close()

	cli := httputil.NewHTTPClient(nil)

	url := ""
	doRequest := func() (*http.Response, error) {
		return cli.Get(url)
	}

	failure := false
	var got interface{}
	readResponseBody := func(resp *http.Response) error {
		failure = false
		if resp.StatusCode != 200 {
			failure = true
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(&got)
	}

	// Check that we really recognize unexpected EOF error by failing on all retries
	url = mockPermanentlyBrokenServer.URL
	_, err := httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, NotNil)
	c.Assert(err, Equals, io.ErrUnexpectedEOF)
	c.Assert(err, ErrorMatches, "unexpected EOF")
	// check that we exhausted all retries (as defined by mocked retry strategy)
	c.Assert(permanentlyBrokenSrvCalls, Equals, 5)
	c.Check(failure, Equals, false)
	c.Check(got, Equals, nil)

	url = mockSomewhatBrokenServer.URL
	failure = false
	got = nil
	// Check that we retry on unexpected EOF and eventually succeed
	_, err = httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, IsNil)
	// check that we retried 4 times
	c.Check(failure, Equals, false)
	c.Check(got, DeepEquals, map[string]interface{}{"ok": true})
	c.Assert(somewhatBrokenSrvCalls, Equals, 4)
}

func (s *retrySuite) TestRetryRequestFailOnReadResponseBody(c *C) {
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<bad>")
		return
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	cli := httputil.NewHTTPClient(nil)

	doRequest := func() (*http.Response, error) {
		return cli.Get(mockServer.URL)
	}

	failure := false
	var got interface{}
	readResponseBody := func(resp *http.Response) error {
		failure = false
		if resp.StatusCode != 200 {
			failure = true
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(&got)
	}

	_, err := httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, ErrorMatches, `invalid character '<' looking for beginning of value`)
	c.Check(failure, Equals, false)
}

func (s *retrySuite) TestRetryRequestReadResponseBodyFailure(c *C) {
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		io.WriteString(w, `{"error": true}`)
		return
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	cli := httputil.NewHTTPClient(nil)

	doRequest := func() (*http.Response, error) {
		return cli.Get(mockServer.URL)
	}

	failure := false
	var got interface{}
	readResponseBody := func(resp *http.Response) error {
		failure = false
		if resp.StatusCode != 200 {
			failure = true
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(&got)
	}

	resp, err := httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, IsNil)
	c.Check(failure, Equals, true)
	c.Check(resp.StatusCode, Equals, 404)
}

func (s *retrySuite) TestRetryRequestTimeoutHandling(c *C) {
	permanentlyBrokenSrvCalls := 0
	somewhatBrokenSrvCalls := 0

	finished := make(chan struct{})

	mockPermanentlyBrokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permanentlyBrokenSrvCalls++
		<-finished
	}))
	c.Assert(mockPermanentlyBrokenServer, NotNil)
	defer mockPermanentlyBrokenServer.Close()

	mockSomewhatBrokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		somewhatBrokenSrvCalls++
		if somewhatBrokenSrvCalls > 2 {
			io.WriteString(w, `{"ok": true}`)
			return
		}
		<-finished
	}))
	c.Assert(mockSomewhatBrokenServer, NotNil)
	defer mockSomewhatBrokenServer.Close()

	defer close(finished)

	cli := httputil.NewHTTPClient(&httputil.ClientOptions{
		Timeout: 50 * time.Millisecond,
	})

	url := ""
	doRequest := func() (*http.Response, error) {
		return cli.Get(url)
	}

	failure := false
	var got interface{}
	readResponseBody := func(resp *http.Response) error {
		failure = false
		if resp.StatusCode != 200 {
			failure = true
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(&got)
	}

	// Check that we really recognize unexpected EOF error by failing on all retries
	url = mockPermanentlyBrokenServer.URL
	_, err := httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `.*Client.Timeout.*`)
	// check that we exhausted all retries (as defined by mocked retry strategy)
	c.Assert(permanentlyBrokenSrvCalls, Equals, 5)
	c.Check(failure, Equals, false)
	c.Check(got, Equals, nil)

	url = mockSomewhatBrokenServer.URL
	failure = false
	got = nil
	// Check that we retry on unexpected EOF and eventually succeed
	_, err = httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, IsNil)
	// check that we retried 4 times
	c.Check(failure, Equals, false)
	c.Check(got, DeepEquals, map[string]interface{}{"ok": true})
	c.Assert(somewhatBrokenSrvCalls, Equals, 3)
}

func (s *retrySuite) TestRetryRequestFailOnDNS(c *C) {
	// TODO: retry some types of DNS errors?

	url := "http://nonexistingserver909123.com/"

	cli := httputil.NewHTTPClient(nil)

	n := 0
	doRequest := func() (*http.Response, error) {
		n++
		return cli.Get(url)
	}

	var got interface{}
	readResponseBody := func(resp *http.Response) error {
		if resp.StatusCode >= 500 {
			return fmt.Errorf("proxy error")
		}
		return json.NewDecoder(resp.Body).Decode(&got)
	}

	_, err := httputil.RetryRequest("endp", doRequest, readResponseBody, testRetryStrategy)
	c.Assert(err, NotNil)
}
