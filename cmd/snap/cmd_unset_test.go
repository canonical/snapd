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
	snapunset "github.com/snapcore/snapd/cmd/snap"
)

func (s *snapSetSuite) TestInvalidUnsetParameters(c *check.C) {
	invalidParameters := []string{"unset"}
	_ := mylog.Check2(snapunset.Parser(snapunset.Client()).ParseArgs(invalidParameters))
	c.Check(err, check.ErrorMatches, "the required arguments `<snap>` and `<conf key> \\(at least 1 argument\\)` were not provided")
	c.Check(s.setConfApiCalls, check.Equals, 0)

	invalidParameters = []string{"unset", "snap-name"}
	_ = mylog.Check2(snapunset.Parser(snapunset.Client()).ParseArgs(invalidParameters))
	c.Check(err, check.ErrorMatches, "the required argument `<conf key> \\(at least 1 argument\\)` was not provided")
	c.Check(s.setConfApiCalls, check.Equals, 0)
}

func (s *snapSetSuite) TestSnapUnset(c *check.C) {
	// expected value is "nil" as the key is unset
	s.mockSetConfigServer(c, nil)

	_ := mylog.Check2(snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "snapname", "key"}))
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *aspectsSuite) TestAspectUnset(c *check.C) {
	restore := s.mockAspectsFlag(c)
	defer restore()

	s.mockAspectServer(c, `{"abc":null}`, false)

	_ := mylog.Check2(snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "foo/bar/baz", "abc"}))
	c.Assert(err, check.IsNil)
}

func (s *aspectsSuite) TestAspectUnsetNoWait(c *check.C) {
	restore := s.mockAspectsFlag(c)
	defer restore()

	s.mockAspectServer(c, `{"abc":null}`, true)

	rest := mylog.Check2(snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "--no-wait", "foo/bar/baz", "abc"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	c.Check(s.Stdout(), check.Equals, "123\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *aspectsSuite) TestAspectUnsetDisabledFlag(c *check.C) {
	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		default:
			mylog.Check(fmt.Errorf("expected to get no requests, now on %d (%v)", reqs+1, r))
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	_ := mylog.Check2(snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "foo/bar/baz", "abc"}))
	c.Assert(err, check.ErrorMatches, "aspect-based configuration is disabled: you must set 'experimental.aspects-configuration' to true")
}

func (s *aspectsSuite) TestAspectUnsetInvalidAspectID(c *check.C) {
	restore := s.mockAspectsFlag(c)
	defer restore()

	_ := mylog.Check2(snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "foo//bar", "abc"}))
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "aspect identifier must conform to format: <account-id>/<bundle>/<aspect>")
}
