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
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"

	"gopkg.in/check.v1"

	snap "github.com/ubuntu-core/snappy/cmd/snap"
)

func (s *SnapSuite) TestInstall(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps/foo.bar")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action":  "install",
				"channel": "chan",
			})
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintln(w, `{"type":"async", "result":{"resource": "/v2/operations/42"}, "status-code": 202}`)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/operations/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "running"}}`)
		case 2:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/operations/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "succeeded"}}`)
		default:
			c.Fatalf("expected to get 3 requests, now on %d", n)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"install", "--channel", "chan", "foo.bar"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 3)
}

func (s *SnapSuite) TestSideload(c *check.C) {
	n := 0
	snapBody := []byte("snap-data")

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			postData, err := ioutil.ReadAll(r.Body)
			c.Assert(err, check.IsNil)
			c.Check(postData, check.DeepEquals, snapBody)
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintln(w, `{"type":"async", "result":{"resource": "/v2/operations/42"}, "status-code": 202}`)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/operations/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "running"}}`)
		case 2:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/operations/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "succeeded"}}`)
		default:
			c.Fatalf("expected to get 3 requests, now on %d", n)
		}

		n++
	})
	snapPath := filepath.Join(c.MkDir(), "foo.snap")
	err := ioutil.WriteFile(snapPath, snapBody, 0644)
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser().ParseArgs([]string{"install", snapPath})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
	// ensure that the fake server api was actually hit
	c.Check(n, check.Equals, 3)
}
