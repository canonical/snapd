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
	"sort"
	"strings"

	. "gopkg.in/check.v1"

	snapset "github.com/snapcore/snapd/cmd/snap"
)

var getTests = []struct {
	args, stdout, stderr, error string
}{{
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
	args:   "get snapname test-key1 test-key2",
	stdout: "{\n\t\"test-key1\": \"test-value1\",\n\t\"test-key2\": 2\n}\n",
}, {
	args:   "get snapname document",
	stderr: "Key:\ndocument.key1\ndocument.key2\n",
}, {
	args:   "get -d snapname document",
	stdout: "{\n\t\"document\": {\n\t\t\"key1\": \"value1\",\n\t\t\"key2\": \"value2\"\n\t}\n}\n",
}}

func (s *SnapSuite) TestSnapGetTests(c *C) {
	s.mockGetConfigServer(c)

	for _, test := range getTests {
		s.stdout.Truncate(0)
		s.stderr.Truncate(0)

		c.Logf("Test: %s", test.args)

		_, err := snapset.Parser().ParseArgs(strings.Fields(test.args))
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(s.Stderr(), Equals, test.stderr)
			c.Check(s.Stdout(), Equals, test.stdout)
		}
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
		case "missing-key":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {}}`)
		case "document":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"document":{"key1":"value1","key2":"value2"}}}`)
		default:
			c.Errorf("unexpected keys %q", query.Get("keys"))
		}
	})
}

func (s *SnapSuite) TestGetDottedKeys(c *C) {
	data := map[string]interface{}{}
	paths := snapset.GetDottedKeys([]string{}, data)
	c.Assert(paths, HasLen, 0)

	data = map[string]interface{}{
		"a": 1,
		"b": map[string]interface{}{
			"c": "x",
			"d": 2,
		},
		"e": []string{"x", "y"},
		"f": nil,
	}
	paths = snapset.GetDottedKeys([]string{}, data)
	sort.Strings(paths)
	c.Assert(paths, DeepEquals, []string{"a", "b.c", "b.d", "e", "f"})

}
