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
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"gopkg.in/check.v1"

	snapunset "github.com/snapcore/snapd/cmd/snap"
)

func (s *snapSetSuite) TestInvalidUnsetParameters(c *check.C) {
	invalidParameters := []string{"unset"}
	_, err := snapunset.Parser(snapunset.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, "the required arguments `<snap>` and `<conf key> \\(at least 1 argument\\)` were not provided")
	c.Check(s.setConfApiCalls, check.Equals, 0)

	invalidParameters = []string{"unset", "snap-name"}
	_, err = snapunset.Parser(snapunset.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, "the required argument `<conf key> \\(at least 1 argument\\)` was not provided")
	c.Check(s.setConfApiCalls, check.Equals, 0)
}

func (s *snapSetSuite) TestSnapUnset(c *check.C) {
	// expected value is "nil" as the key is unset
	s.mockSetConfigServer(c, nil)

	_, err := snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "snapname", "key"})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *confdbSuite) TestConfdbUnset(c *check.C) {
	restore := s.mockConfdbFlag(c)
	defer restore()

	s.mockConfdbServer(c, `{"values":{"abc":null}}`, false)

	_, err := snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "foo/bar/baz", "abc"})
	c.Assert(err, check.IsNil)
}

func (s *confdbSuite) TestConfdbUnsetNoWait(c *check.C) {
	restore := s.mockConfdbFlag(c)
	defer restore()

	s.mockConfdbServer(c, `{"values":{"abc":null}}`, true)

	rest, err := snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "--no-wait", "foo/bar/baz", "abc"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	c.Check(s.Stdout(), check.Equals, "123\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *confdbSuite) TestConfdbUnsetDisabledFlag(c *check.C) {
	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		default:
			err := fmt.Errorf("expected to get no requests, now on %d (%v)", reqs+1, r)
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	_, err := snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "foo/bar/baz", "abc"})
	c.Assert(err, check.ErrorMatches, `the "confdb" feature is disabled: set 'experimental.confdb' to true`)
}

func (s *confdbSuite) TestConfdbUnsetInvalidConfdbID(c *check.C) {
	restore := s.mockConfdbFlag(c)
	defer restore()

	_, err := snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "foo//bar", "abc"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "confdb-schema view id must conform to format: <account-id>/<confdb-schema>/<view>")
}

func (s *confdbSuite) TestConfdbUnsetWaitFor(c *check.C) {
	restore := s.mockConfdbFlag(c)
	defer restore()

	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		case 0:
			c.Check(r.Method, check.Equals, "PUT")
			c.Check(r.URL.Path, check.Equals, "/v2/confdb/foo/bar/baz")

			raw, err := io.ReadAll(r.Body)
			c.Assert(err, check.IsNil)

			var body struct {
				Values  map[string]any `json:"values"`
				Options struct {
					AccessTimeout string `json:"access-timeout"`
				} `json:"options"`
			}

			c.Assert(json.Unmarshal(raw, &body), check.IsNil)
			c.Check(body.Values, check.DeepEquals, map[string]any{"abc": nil})
			c.Check(body.Options.AccessTimeout, check.Equals, "5s")

			w.WriteHeader(202)
			fmt.Fprintln(w, asyncResp)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/123")
			fmt.Fprintf(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}\n`)
		default:
			err := fmt.Errorf("expected to get 2 requests, now on %d (%v)", reqs+1, r)
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	rest, err := snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "foo/bar/baz", "abc", "--wait-for", "5s"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *confdbSuite) TestForbidWaitForWithNonConfdbUnset(c *check.C) {
	restore := s.mockConfdbFlag(c)
	defer restore()

	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		default:
			err := fmt.Errorf("expected to get no requests, now on %d (%v)", reqs+1, r)
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	_, err := snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "some-snap", "abc", "--wait-for", "5s"})
	c.Assert(err, check.ErrorMatches, "cannot use --wait-for in non-confdb write")
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *confdbSuite) TestUnsetEmptyKey(c *check.C) {
	_, err := snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "some-snap", ""})
	c.Assert(err, check.NotNil)
	c.Check(err, check.ErrorMatches, "configuration keys cannot be empty")
}
