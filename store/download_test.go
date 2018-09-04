// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/juju/ratelimit"
	"golang.org/x/net/context"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
)

type downloadSuite struct {
	testutil.BaseTest

	ratelimitReaderUsed bool

	store *Store
}

var _ = Suite(&downloadSuite{})

func (s *downloadSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.store = New(nil, nil)
	s.AddCleanup(MockRatelimitReader(func(r io.Reader, bucket *ratelimit.Bucket) io.Reader {
		s.ratelimitReaderUsed = true
		return r
	}))
	s.ratelimitReaderUsed = false
}

func (s *downloadSuite) TestDownloadTrivial(c *C) {
	canary := "downloaded data"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, canary)
	}))
	defer ts.Close()

	var buf SillyBuffer
	err := download(context.TODO(), "example-name", "", ts.URL, nil, s.store, &buf, 0, nil, nil)
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, canary)
	c.Check(s.ratelimitReaderUsed, Equals, false)
}

func (s *downloadSuite) TestDownloadRateLimited(c *C) {
	canary := "downloaded data"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, canary)
	}))
	defer ts.Close()

	var buf SillyBuffer
	err := download(context.TODO(), "example-name", "", ts.URL, nil, s.store, &buf, 0, nil, &DownloadOptions{RateLimit: 1})
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, canary)
	c.Check(s.ratelimitReaderUsed, Equals, true)
}
