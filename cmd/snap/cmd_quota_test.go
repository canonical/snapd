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
		c.Check(r.URL.Path, check.Equals, "/v2/quotas/foo")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func makeFakeGetQuotaGroupsHandler(c *check.C, body string) func(w http.ResponseWriter, r *http.Request) {
	var called bool
	return func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v2/quotas")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func makeFakeQuotaPostHandler(c *check.C, action, body, groupName, parentName string, snaps []string, maxMemory int64) func(w http.ResponseWriter, r *http.Request) {
	var called bool
	return func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v2/quotas")
		c.Check(r.Method, check.Equals, "POST")

		buf, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)

		switch action {
		case "remove":
			c.Check(string(buf), check.Equals, fmt.Sprintf(`{"action":"remove","group-name":%q}` + "\n", groupName))
		case "ensure":
			var snapNames []string
			for _, sn := range snaps {
				snapNames = append(snapNames, fmt.Sprintf("%q", sn))
			}
			snapsStr := strings.Join(snapNames, ",")
			c.Check(string(buf), check.Equals, fmt.Sprintf(`{"action":"ensure","group-name":%q,"parent":%q,"snaps":[%s],"max-memory":%d}` + "\n", groupName, parentName, snapsStr, maxMemory))
		default:
			c.Fatalf("unexpected action %q", action)
		}
		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func (s *quotaSuite) TestQuotaInvalidArgs(c *check.C) {
	for _, args := range []struct {
		args []string
		err  string
	}{
		{[]string{"quota"}, "the required argument `<group-name>` was not provided"},
		{[]string{"quota", "--memory-max=99B"}, "the required argument `<group-name>` was not provided"},
		{[]string{"quota", "--memory-max=99B", "--max-memory=88B", "foo"}, `cannot use --max-memory and --memory-max together`},
		{[]string{"quota", "--memory-max=99", "foo"}, `cannot parse "99": need a number with a unit as input`},
		{[]string{"quota", "--memory-max=888X", "foo"}, `cannot parse "888X\": try 'kB' or 'MB'`},
		// remove-quota command
		{[]string{"remove-quota"}, "the required argument `<group-name>` was not provided"},
	} {
		s.stdout.Reset()
		s.stderr.Reset()

		_, err := main.Parser(main.Client()).ParseArgs(args.args)
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
	s.RedirectClientToTestServer(makeFakeQuotaPostHandler(c, "ensure", `{"type": "sync", "status-code": 200, "result": []}`, "foo", "bar", []string{"snap-a"}, 999))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"quota", "foo", "--max-memory=999B", "--parent=bar", "snap-a"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *validateSuite) TestRemoveQuotaGroup(c *check.C) {
	s.RedirectClientToTestServer(makeFakeQuotaPostHandler(c, "remove", `{"type": "sync", "status-code": 200, "result": []}`, "foo", "", nil, 0))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"remove-quota", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *quotaSuite) TestGetAllQuotaGroups(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	s.RedirectClientToTestServer(makeFakeGetQuotaGroupsHandler(c,
		`{"type": "sync", "status-code": 200, "result": [
			{"group-name":"aaa","subgroups":["ccc","ddd"],"parent":"zzz","max-memory":1000},
			{"group-name":"ddd","parent":"aaa","max-memory":400},
			{"group-name":"bbb","parent":"zzz","max-memory":1000},
			{"group-name":"yyy","max-memory":1000},
			{"group-name":"zzz","subgroups":["bbb","aaa"],"max-memory":5000},
			{"group-name":"ccc","parent":"aaa","max-memory":400},
			{"group-name":"xxx","max-memory":9900}
			]}`))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"quotas"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals,
		"Quota  Parent  Max-Memory\n" +
		"xxx             9.9kB\n" +
		"yyy             1000B\n" +
		"zzz             5000B\n" +
		"aaa    zzz      1000B\n" +
		"ccc    aaa       400B\n" +
		"ddd    aaa       400B\n" +
		"bbb    zzz      1000B\n")
}

func (s *quotaSuite) TestNoQuotaGroups(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	s.RedirectClientToTestServer(makeFakeGetQuotaGroupsHandler(c,
		`{"type": "sync", "status-code": 200, "result": []}`))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"quotas"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "No quota groups defined.\n")
}
