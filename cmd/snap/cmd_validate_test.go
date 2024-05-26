// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"io"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	main "github.com/snapcore/snapd/cmd/snap"
)

type validateSuite struct {
	BaseSnapSuite
}

var _ = check.Suite(&validateSuite{})

func makeFakeValidationSetPostHandler(c *check.C, body, action string, sequence int) func(w http.ResponseWriter, r *http.Request) {
	var called bool
	return func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v2/validation-sets/foo/bar")
		c.Check(r.Method, check.Equals, "POST")

		buf := mylog.Check2(io.ReadAll(r.Body))
		c.Assert(err, check.IsNil)
		switch {
		case sequence != 0 && action != "forget":
			c.Check(string(buf), check.DeepEquals, fmt.Sprintf("{\"action\":\"apply\",\"mode\":%q,\"sequence\":%d}\n", action, sequence))
		case sequence == 0 && action != "forget":
			c.Check(string(buf), check.DeepEquals, fmt.Sprintf("{\"action\":\"apply\",\"mode\":%q}\n", action))
		case sequence != 0 && action == "forget":
			c.Check(string(buf), check.DeepEquals, fmt.Sprintf("{\"action\":\"forget\",\"sequence\":%d}\n", sequence))
		case action == "forget":
			c.Check(string(buf), check.DeepEquals, "{\"action\":\"forget\"}\n")
		default:
			c.Fatalf("unexpected action: %s", action)
		}

		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func makeFakeValidationSetQueryHandler(c *check.C, body string) func(w http.ResponseWriter, r *http.Request) {
	var called bool
	return func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v2/validation-sets/foo/bar")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func makeFakeListValidationsSetsHandler(c *check.C, body string) func(w http.ResponseWriter, r *http.Request) {
	var called bool
	return func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v2/validation-sets")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func (s *validateSuite) TestValidateInvalidArgs(c *check.C) {
	for _, args := range []struct {
		args []string
		err  string
	}{
		{[]string{"foo"}, `cannot parse validation set "foo": expected a single account/name`},
		{[]string{"foo/bar/baz"}, `cannot parse validation set "foo/bar/baz": expected a single account/name`},
		{[]string{"--monitor", "--enforce"}, `cannot use --monitor and --enforce together`},
		{[]string{"--monitor", "--forget"}, `cannot use --monitor and --forget together`},
		{[]string{"--enforce", "--forget"}, `cannot use --enforce and --forget together`},
		{[]string{"--enforce"}, `missing validation set argument`},
		{[]string{"--monitor"}, `missing validation set argument`},
		{[]string{"--forget"}, `missing validation set argument`},
		{[]string{"--forget", "foo/-"}, `cannot parse validation set "foo/-": invalid validation set name "-"`},
	} {
		s.stdout.Reset()
		s.stderr.Reset()

		_ := mylog.Check2(main.Parser(main.Client()).ParseArgs(append([]string{"validate"}, args.args...)))
		c.Assert(err, check.ErrorMatches, args.err)
	}
}

func (s *validateSuite) TestValidateMonitor(c *check.C) {
	s.RedirectClientToTestServer(makeFakeValidationSetPostHandler(c, `{"type": "sync", "status-code": 200, "result": {"account-id":"foo","name":"bar","mode":"monitor","sequence":3,"valid":false}}`, "monitor", 0))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "--monitor", "foo/bar"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "invalid\n")
}

func (s *validateSuite) TestValidateMonitorPinned(c *check.C) {
	s.RedirectClientToTestServer(makeFakeValidationSetPostHandler(c, `{"type": "sync", "status-code": 200, "result": {"account-id":"foo","name":"bar","mode":"monitor","sequence":3,"valid":true}}}`, "monitor", 3))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "--monitor", "foo/bar=3"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "valid\n")
}

func (s *validateSuite) TestValidateEnforce(c *check.C) {
	s.RedirectClientToTestServer(makeFakeValidationSetPostHandler(c, `{"type": "sync", "status-code": 200, "result": {"account-id":"foo","name":"bar","mode":"enforce","sequence":3,"valid":true}}}`, "enforce", 0))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "--enforce", "foo/bar"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *validateSuite) TestValidateEnforcePinned(c *check.C) {
	s.RedirectClientToTestServer(makeFakeValidationSetPostHandler(c, `{"type": "sync", "status-code": 200, "result": {"account-id":"foo","name":"bar","mode":"enforce","sequence":3,"valid":true}}}`, "enforce", 5))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "--enforce", "foo/bar=5"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *validateSuite) TestValidateForget(c *check.C) {
	s.RedirectClientToTestServer(makeFakeValidationSetPostHandler(c, `{"type": "sync", "status-code": 200, "result": []}`, "forget", 0))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "--forget", "foo/bar"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *validateSuite) TestValidateForgetPinned(c *check.C) {
	s.RedirectClientToTestServer(makeFakeValidationSetPostHandler(c, `{"type": "sync", "status-code": 200, "result": []}`, "forget", 5))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "--forget", "foo/bar=5"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *validateSuite) TestValidateQueryOne(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	s.RedirectClientToTestServer(makeFakeValidationSetQueryHandler(c, `{"type": "sync", "status-code": 200, "result": {"account-id":"foo","name":"bar","mode":"monitor","sequence":3,"valid":true}}`))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "foo/bar"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "valid")
}

func (s *validateSuite) TestValidateQueryOneInvalid(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	s.RedirectClientToTestServer(makeFakeValidationSetQueryHandler(c, `{"type": "sync", "status-code": 200, "result": {"account-id":"foo","name":"bar","mode":"monitor","sequence":3,"valid":false}}`))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "foo/bar"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "invalid")
}

func (s *validateSuite) TestValidationSetsList(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	s.RedirectClientToTestServer(makeFakeListValidationsSetsHandler(c, `{"type": "sync", "status-code": 200, "result": [
		{"account-id":"foo","name":"bar","mode":"monitor","pinned-at":2,"sequence":3,"valid":true},
		{"account-id":"foo","name":"baz","mode":"enforce","sequence":1,"valid":false}
	]}`))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "Validation  Mode     Seq  Current       Notes\n"+
		"foo/bar=2   monitor  3    valid    \n"+
		"foo/baz     enforce  1    invalid  \n",
	)
}

func (s *validateSuite) TestValidationSetsListEmpty(c *check.C) {
	restore := main.MockIsStdinTTY(true)
	defer restore()

	s.RedirectClientToTestServer(makeFakeListValidationsSetsHandler(c, `{"type": "sync", "status-code": 200, "result": []}`))

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "No validations are available\n")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *validateSuite) TestValidateRefreshOnlyUsedWithEnforce(c *check.C) {
	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "--refresh", "--monitor", "foo/bar"}))
	c.Assert(err, check.ErrorMatches, "--refresh can only be used together with --enforce")
	c.Check(rest, check.HasLen, 1)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *validateSuite) TestValidationSetsRefreshEnforce(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {"snap-names": ["one","two"]}}}`)

		default:
			c.Fatalf("expected to get 2 requests, now on %d", n+1)
		}

		n++
	})

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "--refresh", "--enforce", "foo/bar"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "Refreshed/installed snaps \"one\", \"two\" to enforce validation set \"foo/bar\"\n")
}

func (s *validateSuite) TestValidationSetsRefreshEnforceNoUnmetConstraints(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/snaps")
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type": "async", "change": "42", "status-code": 202}`)
		case 1:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/42")
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}`)

		default:
			c.Fatalf("expected to get 2 requests, now on %d", n+1)
		}

		n++
	})

	rest := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"validate", "--refresh", "--enforce", "foo/bar"}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "Enforced validation set \"foo/bar\"\n")
}
