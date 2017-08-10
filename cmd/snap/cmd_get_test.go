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
	args:   "get -l snapname test-key1",
	stdout: "Key        Value\ntest-key1  test-value1\n",
}, {
	args:   "get snapname -l test-key1 test-key2",
	stdout: "Key        Value\ntest-key1  test-value1\ntest-key2  2\n",
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
	args:   "get -d snapname",
	stdout: "{\n\t\"bar\": 100,\n\t\"foo\": {\n\t\t\"key1\": \"value1\",\n\t\t\"key2\": \"value2\"\n\t}\n}\n",
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
		case "":
			fmt.Fprintln(w, `{"type":"sync", "status-code": 200, "result": {"foo":{"key1":"value1","key2":"value2"},"bar":100}}`)
		default:
			c.Errorf("unexpected keys %q", query.Get("keys"))
		}
	})
}
