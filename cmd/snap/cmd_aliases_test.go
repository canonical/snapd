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
	"io/ioutil"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	. "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestAliasesHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] aliases [<snap>]

The aliases command lists all aliases available in the system and their status.

$ snap aliases <snap>

Lists only the aliases defined by the specified snap.

An alias noted as undefined means it was explicitly enabled or disabled but is
not defined in the current revision of the snap; possibly temporarely (e.g
because of a revert), if not this can be cleared with snap alias --reset.

Application Options:
      --version     Print the version and exit

Help Options:
  -h, --help        Show this help message
`
	rest, err := Parser().ParseArgs([]string{"aliases", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestAliases(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/aliases")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": map[string]map[string]client.AliasStatus{
				"foo": {
					"foo0":      {App: "foo", Status: "auto"},
					"foo_reset": {App: "foo.reset", Status: ""},
				},
				"bar": {
					"bar_dump":   {App: "bar.dump", Status: "enabled"},
					"bar_dump.1": {App: "", Status: "disabled"},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"aliases"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"App        Alias       Notes\n" +
		"bar.dump   bar_dump    enabled\n" +
		"bar.???    bar_dump.1  undefined,disabled\n" +
		"foo        foo0        auto\n" +
		"foo.reset  foo_reset   -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAliasesFilterSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/aliases")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": map[string]map[string]client.AliasStatus{
				"foo": {
					"foo0":      {App: "foo", Status: "auto"},
					"foo_reset": {App: "foo.reset", Status: ""},
				},
				"bar": {
					"bar_dump":   {App: "bar.dump", Status: "enabled"},
					"bar_dump.1": {App: "", Status: "disabled"},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"aliases", "foo"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"App        Alias      Notes\n" +
		"foo        foo0       auto\n" +
		"foo.reset  foo_reset  -\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAliasesNone(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/aliases")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type":   "sync",
			"result": map[string]map[string]client.AliasStatus{},
		})
	})
	_, err := Parser().ParseArgs([]string{"aliases"})
	c.Assert(err, IsNil)
	expectedStdout := "" +
		"App  Alias  Notes\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestAliasesSorting(c *C) {
	tests := []struct {
		snap1  string
		app1   string
		alias1 string
		snap2  string
		app2   string
		alias2 string
	}{
		{"bar", "bar", "r", "baz", "baz", "z"},
		{"bar", "bar", "bar0", "bar", "bar.app", "bapp"},
		{"bar", "bar.app1", "bapp1", "bar", "bar.app2", "bapp2"},
		{"bar", "bar", "bar0", "bar", "", "bapp"},
		{"bar", "bar.app1", "appy", "bar", "", "appx"},
		{"bar", "", "bapp1", "bar", "", "bapp2"},
		{"bar", "bar.app1", "appx", "bar", "bar.app1", "appy"},
	}

	for _, test := range tests {
		res := AliasInfoLess(test.snap1, test.alias1, test.app1, test.snap2, test.alias2, test.app2)
		c.Check(res, Equals, true, Commentf("%v", test))

		rres := AliasInfoLess(test.snap2, test.alias2, test.app2, test.snap1, test.alias1, test.app1)
		c.Check(rres, Equals, false, Commentf("reversed %v", test))
	}

}
