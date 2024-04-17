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
	"net/http"
	"strings"

	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"

	snapset "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/strutil"
)

type getCmdArgs struct {
	args, stdout, stderr, error string
	isTerminal                  bool
}

var getTests = []getCmdArgs{{
	args:  "get snap-name --foo",
	error: ".*unknown flag.*foo.*",
}, {
	args:   "get snapname test-key1",
	stdout: "test-value1\n",
}, {
	args:   "get snapname test-key2",
	stdout: "2\n",
}, {
	args:   "get snapname missing-key",
	stdout: "\n",
}, {
	args:   "get -t snapname test-key1",
	stdout: "\"test-value1\"\n",
}, {
	args:   "get -t snapname test-key2",
	stdout: "2\n",
}, {
	args:   "get -t snapname missing-key",
	stdout: "null\n",
}, {
	args:   "get -d snapname test-key1",
	stdout: "{\n\t\"test-key1\": \"test-value1\"\n}\n",
}, {
	args:   "get -l snapname test-key1",
	stdout: "Key        Value\ntest-key1  test-value1\n",
}, {
	args:   "get snapname -l test-key1 test-key2",
	stdout: "Key        Value\ntest-key1  test-value1\ntest-key2  2\n",
}, {
	args:   "get snapname document",
	stderr: `WARNING: The output of 'snap get' will become a list with columns - use -d or -l to force the output format.\n`,
	stdout: "{\n\t\"document\": {\n\t\t\"key1\": \"value1\",\n\t\t\"key2\": \"value2\"\n\t}\n}\n",
}, {
	isTerminal: true,
	args:       "get snapname document",
	stdout:     "Key            Value\ndocument.key1  value1\ndocument.key2  value2\n",
}, {
	args:   "get snapname -d test-key1 test-key2",
	stdout: "{\n\t\"test-key1\": \"test-value1\",\n\t\"test-key2\": 2\n}\n",
}, {
	args:   "get snapname -l document",
	stdout: "Key            Value\ndocument.key1  value1\ndocument.key2  value2\n",
}, {
	args:   "get -d snapname document",
	stdout: "{\n\t\"document\": {\n\t\t\"key1\": \"value1\",\n\t\t\"key2\": \"value2\"\n\t}\n}\n",
}, {
	args:   "get -l snapname",
	stdout: "Key  Value\nbar  100\nfoo  {...}\n",
}, {
	args:   "get snapname -l test-key3 test-key4",
	stdout: "Key          Value\ntest-key3.a  1\ntest-key3.b  2\ntest-key3-a  9\ntest-key4.a  3\ntest-key4.b  4\n",
}, {
	args:   "get -d snapname",
	stdout: "{\n\t\"bar\": 100,\n\t\"foo\": {\n\t\t\"key1\": \"value1\",\n\t\t\"key2\": \"value2\"\n\t}\n}\n",
}, {
	isTerminal: true,
	args:       "get snapname  test-key1 test-key2",
	stdout:     "Key        Value\ntest-key1  test-value1\ntest-key2  2\n",
}, {
	isTerminal: false,
	args:       "get snapname  test-key1 test-key2",
	stdout:     "{\n\t\"test-key1\": \"test-value1\",\n\t\"test-key2\": 2\n}\n",
	stderr:     `WARNING: The output of 'snap get' will become a list with columns - use -d or -l to force the output format.\n`,
},
}

func (s *SnapSuite) runTests(cmds []getCmdArgs, c *C) {
	for _, test := range cmds {
		s.stdout.Truncate(0)
		s.stderr.Truncate(0)

		c.Logf("Test: %s", test.args)

		restore := snapset.MockIsStdinTTY(test.isTerminal)
		defer restore()

		_, err := snapset.Parser(snapset.Client()).ParseArgs(strings.Fields(test.args))
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(s.Stderr(), Equals, test.stderr)
			c.Check(s.Stdout(), Equals, test.stdout)
		}
	}
}

func (s *SnapSuite) TestSnapGetTests(c *C) {
	s.mockGetConfigServer(c)
	s.runTests(getTests, c)
}

var getNoConfigTests = []getCmdArgs{{
	args:  "get -l snapname",
	error: `snap "snapname" has no configuration`,
}, {
	args:  "get snapname",
	error: `snap "snapname" has no configuration`,
}, {
	args:   "get -d snapname",
	stdout: "{}\n",
}}

func (s *SnapSuite) TestSnapGetNoConfiguration(c *C) {
	s.mockGetEmptyConfigServer(c)
	s.runTests(getNoConfigTests, c)
}

func (s *SnapSuite) TestSortByPath(c *C) {
	values := []snapset.ConfigValue{
		{Path: "test-key3.b"},
		{Path: "a"},
		{Path: "test-key3.a"},
		{Path: "a.b.c"},
		{Path: "test-key4.a"},
		{Path: "test-key4.b"},
		{Path: "a-b"},
		{Path: "zzz"},
		{Path: "aa"},
		{Path: "test-key3-a"},
		{Path: "a.b"},
	}
	snapset.SortByPath(values)

	expected := []string{
		"a",
		"a.b",
		"a.b.c",
		"a-b",
		"aa",
		"test-key3.a",
		"test-key3.b",
		"test-key3-a",
		"test-key4.a",
		"test-key4.b",
		"zzz",
	}

	c.Assert(values, HasLen, len(expected))

	for i, e := range expected {
		c.Assert(values[i].Path, Equals, e)
	}
}

func (s *SnapSuite) mockGetConfigServer(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/snaps/snapname/conf" {
			c.Errorf("unexpected path %q", r.URL.Path)
			return
		}

		c.Check(r.Method, Equals, "GET")

		query := r.URL.Query()
		switch query.Get("keys") {
		case "test-key1":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"test-key1":"test-value1"}}`)
		case "test-key2":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"test-key2":2}}`)
		case "test-key1,test-key2":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"test-key1":"test-value1","test-key2":2}}`)
		case "test-key3,test-key4":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"test-key3":{"a":1,"b":2},"test-key3-a":9,"test-key4":{"a":3,"b":4}}}`)
		case "missing-key":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {}}`)
		case "document":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"document":{"key1":"value1","key2":"value2"}}}`)
		case "":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"foo":{"key1":"value1","key2":"value2"},"bar":100}}`)
		default:
			c.Errorf("unexpected keys %q", query.Get("keys"))
		}
	})
}

func (s *SnapSuite) mockGetEmptyConfigServer(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/snaps/snapname/conf" {
			c.Errorf("unexpected path %q", r.URL.Path)
			return
		}

		c.Check(r.Method, Equals, "GET")

		fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {}}`)
	})
}

const syncResp = `{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": %s
}`

func (s *aspectsSuite) TestAspectGet(c *C) {
	restore := snapset.MockIsStdinTTY(true)
	defer restore()

	restore = s.mockAspectsFlag(c)
	defer restore()

	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/aspects/foo/bar/baz")

			q := r.URL.Query()
			fields := strutil.CommaSeparatedList(q.Get("fields"))
			c.Check(fields, DeepEquals, []string{"abc"})

			w.WriteHeader(200)
			fmt.Fprintf(w, syncResp, `{"abc": "cba"}`)
		default:
			err := fmt.Errorf("expected to get 1 request, now on %d (%v)", reqs+1, r)
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	rest, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"get", "foo/bar/baz", "abc"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "cba\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *aspectsSuite) TestAspectGetAsDocument(c *C) {
	restore := snapset.MockIsStdinTTY(true)
	defer restore()

	restore = s.mockAspectsFlag(c)
	defer restore()

	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/aspects/foo/bar/baz")

			q := r.URL.Query()
			fields := strutil.CommaSeparatedList(q.Get("fields"))
			c.Check(fields, DeepEquals, []string{"abc"})

			w.WriteHeader(200)
			fmt.Fprintf(w, syncResp, `{"abc": "cba"}`)
		default:
			err := fmt.Errorf("expected to get 1 request, now on %d (%v)", reqs+1, r)
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	rest, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"get", "-d", "foo/bar/baz", "abc"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)

	c.Check(s.Stdout(), Equals, `{
	"abc": "cba"
}
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *aspectsSuite) TestAspectGetMany(c *C) {
	restore := snapset.MockIsStdinTTY(true)
	defer restore()

	restore = s.mockAspectsFlag(c)
	defer restore()

	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/aspects/foo/bar/baz")

			q := r.URL.Query()
			fields := strutil.CommaSeparatedList(q.Get("fields"))
			c.Check(fields, DeepEquals, []string{"abc", "xyz"})

			w.WriteHeader(200)
			fmt.Fprintf(w, syncResp, `{"abc": 1, "xyz": false}`)
		default:
			err := fmt.Errorf("expected to get 1 request, now on %d (%v)", reqs+1, r)
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	rest, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"get", "foo/bar/baz", "abc", "xyz"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals,
		`Key  Value
abc  1
xyz  false
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *aspectsSuite) TestAspectGetManyAsDocument(c *C) {
	restore := snapset.MockIsStdinTTY(true)
	defer restore()

	restore = s.mockAspectsFlag(c)
	defer restore()

	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/aspects/foo/bar/baz")

			q := r.URL.Query()
			fields := strutil.CommaSeparatedList(q.Get("fields"))
			c.Check(fields, DeepEquals, []string{"abc", "xyz"})

			w.WriteHeader(200)
			fmt.Fprintf(w, syncResp, `{"abc": 1, "xyz": false}`)
		default:
			err := fmt.Errorf("expected to get 1 request, now on %d (%v)", reqs+1, r)
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	rest, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"get", "-d", "foo/bar/baz", "abc", "xyz"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)

	c.Check(s.Stdout(), Equals, `{
	"abc": 1,
	"xyz": false
}
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *aspectsSuite) TestAspectGetInvalidAspectID(c *check.C) {
	restore := s.mockAspectsFlag(c)
	defer restore()

	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"get", "foo//bar", "foo=bar"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "aspect identifier must conform to format: <account-id>/<bundle>/<aspect>")
}

func (s *aspectsSuite) TestAspectGetDisabledFlag(c *check.C) {
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

	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"get", "foo/bar/baz", "abc"})
	c.Assert(err, check.ErrorMatches, "aspect-based configuration is disabled: you must set 'experimental.aspects-configuration' to true")
}

func (s *aspectsSuite) TestAspectGetNoFields(c *check.C) {
	restore := s.mockAspectsFlag(c)
	defer restore()

	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		case 0:
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/aspects/foo/bar/baz")

			fields := r.URL.Query().Get("fields")
			c.Check(fields, Equals, "")

			w.WriteHeader(200)
			fmt.Fprintf(w, syncResp, `{"abc": 1, "xyz": false}`)
		default:
			err := fmt.Errorf("expected to get 1 request, now on %d (%v)", reqs+1, r)
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	rest, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"get", "foo/bar/baz"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)

	c.Check(s.Stdout(), Equals, `{
	"abc": 1,
	"xyz": false
}
`)
}
