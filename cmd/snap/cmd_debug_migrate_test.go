// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

	snap "github.com/snapcore/snapd/cmd/snap"
	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"
)

type MigrateHomeSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&MigrateHomeSuite{})

// failRequest logs an error message, fails the test and returns a proper error
// to the client. Use this instead of panic() or c.Fatal() because those crash
// the server and leave the client hanging/retrying.
func failRequest(msg string, w http.ResponseWriter, c *C) {
	c.Error(msg)
	w.WriteHeader(400)
	fmt.Fprintf(w, `{"type": "error", "status-code": 400, "result": {"message": %q}}`, msg)
}

func serverWithChange(chgRsp string, c *C) func(w http.ResponseWriter, r *http.Request) {
	var n int
	return func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/debug")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action": "migrate-home",
				"snaps":  []interface{}{"foo"},
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "status-code": 202, "result": {}, "change": "12"}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/12")
			fmt.Fprint(w, chgRsp)

		default:
			failRequest(fmt.Sprintf("server expected to get 2 requests, now on %d", n+1), w, c)
		}

		n++
	}
}

func (s *MigrateHomeSuite) TestMigrateHome(c *C) {
	rsp := serverWithChange(`{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-names": ["foo"]}}}\n`, c)
	s.RedirectClientToTestServer(rsp)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "migrate-home", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "foo's home directory was migrated to ~/Snap\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *MigrateHomeSuite) TestMigrateHomeManySnaps(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/debug")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"action": "migrate-home",
				"snaps":  []interface{}{"foo", "bar"},
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "status-code": 202, "result": {}, "change": "12"}`)

		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/12")
			fmt.Fprintf(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-names": ["foo", "bar"]}}}\n`)

		default:
			failRequest(fmt.Sprintf("server expected to get 2 requests, now on %d", n+1), w, c)
		}

		n++
	})

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "migrate-home", "foo", "bar"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "\"foo\", \"bar\" migrated their home directories to ~/Snap\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *MigrateHomeSuite) TestMigrateHomeNoSnaps(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		failRequest("unexpected request on server", w, c)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "migrate-home"})
	c.Assert(err, check.ErrorMatches, "the required argument .* was not provided")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *MigrateHomeSuite) TestMigrateHomeServerError(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprintf(w, `{"type": "error", "status-code": 500, "result": {"message": "boom"}}`)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "migrate-home", "foo"})
	c.Assert(err, check.ErrorMatches, "boom")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *MigrateHomeSuite) TestMigrateHomeBadChangeNoSnaps(c *C) {
	// broken change response: missing required "snap-names"
	srv := serverWithChange(`{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-names": []}}}\n`, c)
	s.RedirectClientToTestServer(srv)

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "migrate-home", "foo"})
	c.Assert(err, check.ErrorMatches, `expected "migrate-home" change to have non-empty "snap-names"`)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *MigrateHomeSuite) TestMigrateHomeBadChangeNoData(c *C) {
	// broken change response: missing data
	srv := serverWithChange(`{"type": "sync", "result": {"ready": true, "status": "Done"}}\n`, c)
	s.RedirectClientToTestServer(srv)

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "migrate-home", "foo"})
	c.Assert(err, check.ErrorMatches, `cannot get "snap-names" from change`)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
