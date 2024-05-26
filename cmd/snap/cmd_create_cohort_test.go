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
	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestCreateCohort(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		fmt.Fprintln(w, `{
"type": "sync",
"status-code": 200,
"status": "OK",
"result": {"foo": "what", "bar": "this"}}`)
	})

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-cohort", "foo", "bar"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)

	var v map[string]map[string]map[string]string
	c.Assert(yaml.Unmarshal(s.stdout.Bytes(), &v), check.IsNil)
	c.Check(v, check.DeepEquals, map[string]map[string]map[string]string{
		"cohorts": {
			"foo": {"cohort-key": "what"},
			"bar": {"cohort-key": "this"},
		},
	})
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestCreateCohortNoSnaps(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		panic("shouldn't be called")
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-cohort"}))
	c.Check(err, check.ErrorMatches, "the required argument .* was not provided")
}

func (s *SnapSuite) TestCreateCohortNotFound(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "snap not found", "kind": "snap-not-found"}, "status-code": 404}`)
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-cohort", "foo", "bar"}))
	c.Check(err, check.ErrorMatches, "cannot create cohorts: snap not found")
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestCreateCohortError(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "something went wrong"}}`)
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-cohort", "foo", "bar"}))
	c.Check(err, check.ErrorMatches, "cannot create cohorts: something went wrong")
	c.Check(n, check.Equals, 1)
}
