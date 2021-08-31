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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/jsonutil"
)

type quotaSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&quotaSuite{})

func makeFakeGetQuotaGroupNotFoundHandler(c *check.C, group string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/quotas/"+group)
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(404)
		fmt.Fprintln(w, `{
			"result": {
				"message": "not found"
			},
			"status": "Not Found",
			"status-code": 404,
			"type": "error"
		}`)
	}

}

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

func dispatchFakeHandlers(c *check.C, routes map[string]http.HandlerFunc) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if router, ok := routes[r.URL.Path]; ok {
			router(w, r)
			return
		}
		c.Errorf("unexpected call to %s", r.URL.Path)
	}
}

type fakeQuotaGroupPostHandlerOpts struct {
	action     string
	body       string
	groupName  string
	parentName string
	snaps      []string
	maxMemory  int64
}

type quotasEnsureBody struct {
	Action      string                 `json:"action"`
	GroupName   string                 `json:"group-name,omitempty"`
	ParentName  string                 `json:"parent,omitempty"`
	Snaps       []string               `json:"snaps,omitempty"`
	Constraints map[string]interface{} `json:"constraints,omitempty"`
}

func makeFakeQuotaPostHandler(c *check.C, opts fakeQuotaGroupPostHandlerOpts) func(w http.ResponseWriter, r *http.Request) {
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

		switch opts.action {
		case "remove":
			c.Check(string(buf), check.Equals, fmt.Sprintf(`{"action":"remove","group-name":%q}`+"\n", opts.groupName))
		case "ensure":
			exp := quotasEnsureBody{
				Action:      "ensure",
				GroupName:   opts.groupName,
				ParentName:  opts.parentName,
				Snaps:       opts.snaps,
				Constraints: map[string]interface{}{},
			}
			if opts.maxMemory != 0 {
				exp.Constraints["memory"] = json.Number(fmt.Sprintf("%d", opts.maxMemory))
			}

			postJSON := quotasEnsureBody{}
			err := jsonutil.DecodeWithNumber(bytes.NewReader(buf), &postJSON)
			c.Assert(err, check.IsNil)
			c.Assert(postJSON, check.DeepEquals, exp)
		default:
			c.Fatalf("unexpected action %q", opts.action)
		}
		w.WriteHeader(202)
		fmt.Fprintln(w, opts.body)
	}
}

func makeChangesHandler(c *check.C) func(w http.ResponseWriter, r *http.Request) {
	n := 0
	return func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"status": "Doing"}}`)
		case 2:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)
		default:
			c.Fatalf("expected to get 2 requests, now on %d", n+1)
		}
	}
}

func (s *quotaSuite) TestSetQuotaInvalidArgs(c *check.C) {
	for _, args := range []struct {
		args []string
		err  string
	}{
		{[]string{"set-quota"}, "the required argument `<group-name>` was not provided"},
		{[]string{"set-quota", "--memory=99B"}, "the required argument `<group-name>` was not provided"},
		{[]string{"set-quota", "--memory=99", "foo"}, `cannot parse "99": need a number with a unit as input`},
		{[]string{"set-quota", "--memory=888X", "foo"}, `cannot parse "888X\": try 'kB' or 'MB'`},
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

	const json = `{
		"type": "sync",
		"status-code": 200,
		"result": {
			"group-name":"foo",
			"parent":"bar",
			"subgroups":["subgrp1"],
			"snaps":["snap-a","snap-b"],
			"constraints": { "memory": 1000 },
			"current": { "memory": 900 }
		}
	}`

	s.RedirectClientToTestServer(makeFakeGetQuotaGroupHandler(c, json))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"quota", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, `
name:    foo
parent:  bar
constraints:
  memory:  1000B
current:
  memory:  900B
subgroups:
  - subgrp1
snaps:
  - snap-a
  - snap-b
`[1:])
}

func (s *quotaSuite) TestGetQuotaGroupSimple(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	const jsonTemplate = `{
		"type": "sync",
		"status-code": 200,
		"result": {
			"group-name": "foo",
			"constraints": {"memory": 1000},
			"current": {"memory": %d}
		}
	}`

	s.RedirectClientToTestServer(makeFakeGetQuotaGroupHandler(c, fmt.Sprintf(jsonTemplate, 0)))

	outputTemplate := `
name:  foo
constraints:
  memory:  1000B
current:
  memory:  %dB
`[1:]

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"quota", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, fmt.Sprintf(outputTemplate, 0))

	s.stdout.Reset()
	s.stderr.Reset()

	s.RedirectClientToTestServer(makeFakeGetQuotaGroupHandler(c, fmt.Sprintf(jsonTemplate, 500)))

	rest, err = main.Parser(main.Client()).ParseArgs([]string{"quota", "foo"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, fmt.Sprintf(outputTemplate, 500))
}

func (s *quotaSuite) TestSetQuotaGroupCreateNew(c *check.C) {
	const postJSON = `{"type": "async", "status-code": 202,"change":"42", "result": []}`
	fakeHandlerOpts := fakeQuotaGroupPostHandlerOpts{
		action:     "ensure",
		body:       postJSON,
		groupName:  "foo",
		parentName: "bar",
		snaps:      []string{"snap-a"},
		maxMemory:  999,
	}

	routes := map[string]http.HandlerFunc{
		"/v2/quotas": makeFakeQuotaPostHandler(
			c,
			fakeHandlerOpts,
		),
		// the foo quota group is not found since it doesn't exist yet
		"/v2/quotas/foo": makeFakeGetQuotaGroupNotFoundHandler(c, "foo"),

		"/v2/changes/42": makeChangesHandler(c),
	}

	s.RedirectClientToTestServer(dispatchFakeHandlers(c, routes))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"set-quota", "foo", "--memory=999B", "--parent=bar", "snap-a"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *quotaSuite) TestSetQuotaGroupUpdateExistingUnhappy(c *check.C) {
	const exists = true
	s.testSetQuotaGroupUpdateExistingUnhappy(c, "no options set to change quota group", exists)
}

func (s *quotaSuite) TestSetQuotaGroupCreateNewUnhappy(c *check.C) {
	const exists = false
	s.testSetQuotaGroupUpdateExistingUnhappy(c, "cannot create quota group without memory limit", exists)
}

func (s *quotaSuite) TestSetQuotaGroupCreateNewUnhappyWithParent(c *check.C) {
	const exists = false
	s.testSetQuotaGroupUpdateExistingUnhappy(c, "cannot create quota group without memory limit", exists, "--parent=bar")
}

func (s *quotaSuite) TestSetQuotaGroupUpdateExistingUnhappyWithParent(c *check.C) {
	const exists = true
	s.testSetQuotaGroupUpdateExistingUnhappy(c, "cannot move a quota group to a new parent", exists, "--parent=bar")
}

func (s *quotaSuite) testSetQuotaGroupUpdateExistingUnhappy(c *check.C, errPattern string, exists bool, args ...string) {
	if exists {
		// existing group has 1000 memory limit
		const getJson = `{
			"type": "sync",
			"status-code": 200,
			"result": {
				"group-name":"foo",
				"current": {
					"memory": 500
				},
				"constraints": {
					"memory": 1000
				}
			}
		}`

		s.RedirectClientToTestServer(makeFakeGetQuotaGroupHandler(c, getJson))
	} else {
		s.RedirectClientToTestServer(makeFakeGetQuotaGroupNotFoundHandler(c, "foo"))
	}

	cmdArgs := append([]string{"set-quota", "foo"}, args...)
	_, err := main.Parser(main.Client()).ParseArgs(cmdArgs)
	c.Assert(err, check.ErrorMatches, errPattern)
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *quotaSuite) TestSetQuotaGroupUpdateExisting(c *check.C) {
	const postJSON = `{"type": "async", "status-code": 202,"change":"42", "result": []}`
	fakeHandlerOpts := fakeQuotaGroupPostHandlerOpts{
		action:    "ensure",
		body:      postJSON,
		groupName: "foo",
		maxMemory: 2000,
	}

	const getJsonTemplate = `{
		"type": "sync",
		"status-code": 200,
		"result": {
			"group-name":"foo",
			"constraints": { "memory": %d },
			"current": { "memory": 500 }
		}
	}`

	routes := map[string]http.HandlerFunc{
		"/v2/quotas": makeFakeQuotaPostHandler(
			c,
			fakeHandlerOpts,
		),
		"/v2/quotas/foo": makeFakeGetQuotaGroupHandler(c, fmt.Sprintf(getJsonTemplate, 1000)),
		"/v2/changes/42": makeChangesHandler(c),
	}

	s.RedirectClientToTestServer(dispatchFakeHandlers(c, routes))

	// increase the memory limit to 2000
	rest, err := main.Parser(main.Client()).ParseArgs([]string{"set-quota", "foo", "--memory=2000B"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")

	s.stdout.Reset()
	s.stderr.Reset()

	fakeHandlerOpts2 := fakeQuotaGroupPostHandlerOpts{
		action:    "ensure",
		body:      postJSON,
		groupName: "foo",
		snaps:     []string{"some-snap"},
	}

	routes = map[string]http.HandlerFunc{
		"/v2/quotas": makeFakeQuotaPostHandler(
			c,
			fakeHandlerOpts2,
		),
		// the group was updated to have a 2000 memory limit now
		"/v2/quotas/foo": makeFakeGetQuotaGroupHandler(c, fmt.Sprintf(getJsonTemplate, 2000)),

		"/v2/changes/42": makeChangesHandler(c),
	}

	s.RedirectClientToTestServer(dispatchFakeHandlers(c, routes))

	// add a snap to the group
	rest, err = main.Parser(main.Client()).ParseArgs([]string{"set-quota", "foo", "some-snap"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *quotaSuite) TestRemoveQuotaGroup(c *check.C) {
	const json = `{"type": "async", "status-code": 202,"change": "42"}`
	fakeHandlerOpts := fakeQuotaGroupPostHandlerOpts{
		action:    "remove",
		body:      json,
		groupName: "foo",
	}

	routes := map[string]http.HandlerFunc{
		"/v2/quotas": makeFakeQuotaPostHandler(c, fakeHandlerOpts),

		"/v2/changes/42": makeChangesHandler(c),
	}

	s.RedirectClientToTestServer(dispatchFakeHandlers(c, routes))

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
			{"group-name":"aaa","subgroups":["ccc","ddd","fff"],"parent":"zzz","constraints":{"memory":1000}},
			{"group-name":"ddd","parent":"aaa","constraints":{"memory":400}},
			{"group-name":"bbb","parent":"zzz","constraints":{"memory":1000},"current":{"memory":400}},
			{"group-name":"yyyyyyy","constraints":{"memory":1000}},
			{"group-name":"zzz","subgroups":["bbb","aaa"],"constraints":{"memory":5000}},
			{"group-name":"ccc","parent":"aaa","constraints":{"memory":400}},
			{"group-name":"fff","parent":"aaa","constraints":{"memory":1000},"current":{"memory":0}},
			{"group-name":"xxx","constraints":{"memory":9900},"current":{"memory":10000}}
			]}`))

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"quotas"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, `
Quota    Parent  Constraints   Current
xxx              memory=9.9kB  memory=10.0kB
yyyyyyy          memory=1000B  
zzz              memory=5000B  
aaa      zzz     memory=1000B  
ccc      aaa     memory=400B   
ddd      aaa     memory=400B   
fff      aaa     memory=1000B  
bbb      zzz     memory=1000B  memory=400B
`[1:])
}

func (s *quotaSuite) TestGetAllQuotaGroupsInconsistencyError(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	s.RedirectClientToTestServer(makeFakeGetQuotaGroupsHandler(c,
		`{"type": "sync", "status-code": 200, "result": [
			{"group-name":"aaa","subgroups":["ccc"],"max-memory":1000}]}`))

	_, err := main.Parser(main.Client()).ParseArgs([]string{"quotas"})
	c.Assert(err, check.ErrorMatches, `internal error: inconsistent groups received, unknown subgroup "ccc"`)
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
