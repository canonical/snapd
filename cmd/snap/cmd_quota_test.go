// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"strings"

	main "github.com/snapcore/snapd/cmd/snap"
	"gopkg.in/check.v1"
)

type quotaSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&quotaSuite{})

func makeFakeGetQuotaGroupHandler(c *check.C, body string) func(w http.ResponseWriter, r *http.Request) {
	var called bool
	return func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v2/quota/foo")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func makeFakeQuotaPostHandler(c *check.C, body, groupName, parentName string, snaps []string, maxMemory int64) func(w http.ResponseWriter, r *http.Request) {
	var called bool
	return func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v2/quota")
		c.Check(r.Method, check.Equals, "POST")

		buf, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)

		var snapNames []string
		for _, sn := range snaps {
			snapNames = append(snapNames, fmt.Sprintf("%q", sn))
		}
		snapsStr := strings.Join(snapNames, ",")
		c.Check(string(buf), check.DeepEquals, fmt.Sprintf("{\"group-name\":%q,\"parent\":%q,\"snaps\":[%s],\"max-memory\":%d}\n", groupName, parentName, snapsStr, maxMemory))

		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func (s *quotaSuite) TestQuotaInvalidArgs(c *check.C) {
	for _, args := range []struct {
		args []string
		err  string
	}{
		{[]string{""}, `cannot get quota group without a name`},
		{[]string{"--memory-max=99B"}, "the required argument `<group-name>` was not provided"},
		{[]string{"--memory-max=99B", "--max-memory=88B", "foo"}, `cannot use --max-memory and --memory-max together`},
		{[]string{"--memory-max=99", "foo"}, `cannot parse "99": need a number with a unit as input`},
		{[]string{"--memory-max=888X", "foo"}, `cannot parse "888X\": try 'kB' or 'MB'`},
	} {
		s.stdout.Reset()
		s.stderr.Reset()

		_, err := main.Parser(main.Client()).ParseArgs(append([]string{"quota"}, args.args...))
		c.Assert(err, check.ErrorMatches, args.err)
	}
}

func (s *quotaSuite) TestGetQuotaGroup(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	s.RedirectClientToTestServer(makeFakeGetQuotaGroupHandler(c, `{"type": "sync", "status-code": 200, "result": {"group-name":"foo","parent":"bar","subgroups":["subgrp1"],"snaps":["snap-a","snap-b"],"max-memory":1000}}`))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"quota", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "name: foo\n"+
		"parent: bar\n"+
		"subgroups:\n"+
		"  - subgrp1\n"+
		"max-memory:  1000B\n"+
		"snaps:\n"+
		"  - snap-a\n"+
		"  - snap-b\n")
}

func (s *quotaSuite) TestGetQuotaGroupSimple(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	s.RedirectClientToTestServer(makeFakeGetQuotaGroupHandler(c, `{"type": "sync", "status-code": 200, "result": {"group-name":"foo","max-memory":1000}}`))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"quota", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "name: foo\n"+
		"max-memory:  1000B\n")
}

func (s *validateSuite) TestCreateQuotaGroup(c *check.C) {
	s.RedirectClientToTestServer(makeFakeQuotaPostHandler(c, `{"type": "sync", "status-code": 200, "result": []}`, "foo", "bar", []string{"snap-a"}, 999))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"quota", "foo", "--max-memory=999B", "--parent=bar", "snap-a"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}
