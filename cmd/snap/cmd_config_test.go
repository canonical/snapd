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
	"io/ioutil"
	"net/http"

	"gopkg.in/check.v1"

	snap "github.com/ubuntu-core/snappy/cmd/snap"
	"os"
)

func (s *SnapSuite) TestConfigGetSnippet(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "GET")
		c.Check(r.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
		c.Check(r.URL.Query().Get("snippet"), check.Equals, "baz")

		fmt.Fprintln(w, `{"type": "sync", "result": "hello!"}`)
	})

	rest, err := snap.Parser().ParseArgs([]string{"config", "foo.bar", "baz"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "hello!\n")
}

func (s *SnapSuite) TestConfigSetSnippet(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "PATCH")
		c.Check(r.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
		c.Check(r.URL.Query(), check.HasLen, 0)
		c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
			"baz": "quux",
		})

		fmt.Fprintln(w, `{"type": "sync", "result": "quuux"}`)
	})

	rest, err := snap.Parser().ParseArgs([]string{"config", "foo.bar", "baz=quux"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "quuux\n")
}

func (s *SnapSuite) TestConfigGetFile(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "GET")
		c.Check(r.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
		c.Check(r.URL.Query(), check.HasLen, 0)

		fmt.Fprintln(w, `{"type": "sync", "result": "config:\n  foo.bar:\n    baz: true\n"}`)
	})
	rest, err := snap.Parser().ParseArgs([]string{"config", "foo.bar"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "config:\n  foo.bar:\n    baz: true\n")
}

func (s *SnapSuite) TestConfigSetFile(c *check.C) {
	cfg := "config:\n  foo.bar:\n    baz: false\n"

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "PUT")
		c.Check(r.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
		c.Check(r.URL.Query(), check.HasLen, 0)
		bs, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Check(string(bs), check.Equals, cfg)

		fmt.Fprintln(w, `{"type": "sync", "result": "config:\n  foo.bar:\n    baz: true\n"}`)
	})

	tmpf, err := ioutil.TempFile(c.MkDir(), "cfg")
	c.Assert(err, check.IsNil)
	defer os.Remove(tmpf.Name())
	_, err = tmpf.Write([]byte(cfg))
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser().ParseArgs([]string{"config", "--file", tmpf.Name(), "foo.bar"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "config:\n  foo.bar:\n    baz: true\n")
}

func (s *SnapSuite) TestConfigSetFileStdin(c *check.C) {
	cfg := "config:\n  foo.bar:\n    baz: false\n"

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "PUT")
		c.Check(r.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
		c.Check(r.URL.Query(), check.HasLen, 0)
		bs, err := ioutil.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Check(string(bs), check.Equals, cfg)

		fmt.Fprintln(w, `{"type": "sync", "result": "config:\n  foo.bar:\n    baz: true\n"}`)
	})

	tmpf, err := ioutil.TempFile(c.MkDir(), "cfg")
	c.Assert(err, check.IsNil)
	defer os.Remove(tmpf.Name())
	_, err = tmpf.Write([]byte(cfg))
	c.Assert(err, check.IsNil)

	snap.Stdin, err = os.Open(tmpf.Name())
	c.Assert(err, check.IsNil)

	rest, err := snap.Parser().ParseArgs([]string{"config", "--file", "-", "foo.bar"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, "config:\n  foo.bar:\n    baz: true\n")
}
