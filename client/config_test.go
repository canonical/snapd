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

package client_test

import (
	"encoding/json"
	"io/ioutil"
	"strings"

	"gopkg.in/check.v1"
)

func (cs *clientSuite) TestClientSetConfigFromSnippet(c *check.C) {
	cs.rsp = `{"type": "sync", "result": "42"}`

	out, err := cs.cli.ConfigFromSnippet("foo.bar", "baz", `quux`)
	c.Assert(err, check.IsNil)
	c.Check(out, check.Equals, `"42"`) // quoted because otherwise it'd be a number
	c.Check(cs.req.Method, check.Equals, "PATCH")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
	c.Check(cs.req.Form, check.HasLen, 0)
	c.Assert(cs.req.Body, check.NotNil)

	dec := json.NewDecoder(cs.req.Body)
	var m map[string]interface{}
	c.Assert(dec.Decode(&m), check.IsNil)
	c.Check(m, check.DeepEquals, map[string]interface{}{"baz": "quux"})
}

func (cs *clientSuite) TestClientSetConfigBarewordFromSnippet(c *check.C) {
	cs.rsp = `{"type": "sync", "result": "meh"}`

	out, err := cs.cli.ConfigFromSnippet("foo.bar", "baz", `quux`)
	c.Assert(err, check.IsNil)
	c.Check(out, check.Equals, "meh")
	c.Check(cs.req.Method, check.Equals, "PATCH")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
	c.Check(cs.req.Form, check.HasLen, 0)
	c.Assert(cs.req.Body, check.NotNil)

	dec := json.NewDecoder(cs.req.Body)
	var m map[string]interface{}
	c.Assert(dec.Decode(&m), check.IsNil)
	c.Check(m, check.DeepEquals, map[string]interface{}{"baz": "quux"})
}

func (cs *clientSuite) TestClientSetConfigNumberFromSnippet(c *check.C) {
	cs.rsp = `{"type": "sync", "result": 42}`

	out, err := cs.cli.ConfigFromSnippet("foo.bar", "baz", `43`)
	c.Assert(err, check.IsNil)
	c.Check(out, check.Equals, `42`) // not quoted
	c.Check(cs.req.Method, check.Equals, "PATCH")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
	c.Check(cs.req.Form, check.HasLen, 0)
	c.Assert(cs.req.Body, check.NotNil)

	dec := json.NewDecoder(cs.req.Body)
	var m map[string]interface{}
	c.Assert(dec.Decode(&m), check.IsNil)
	c.Check(m, check.DeepEquals, map[string]interface{}{"baz": 43.})
}

func (cs *clientSuite) TestClientGetConfigFromSnippet(c *check.C) {
	cs.rsp = `{"type": "sync", "result": "hello"}`

	out, err := cs.cli.ConfigFromSnippet("foo.bar", "baz", "")
	c.Assert(err, check.IsNil)
	c.Check(out, check.Equals, "hello")
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
	c.Check(cs.req.URL.Query().Get("snippet"), check.Equals, "baz")
}

func (cs *clientSuite) TestClientSetConfigFromReader(c *check.C) {
	cfg := "config:\n foo:\n  baz: 42"
	cs.rsp = `{"type": "sync", "result": "` + strings.Replace(cfg, "\n", "\\n", -1) + `"}`

	f, err := cs.cli.ConfigFromReader("foo.bar", strings.NewReader(cfg))
	c.Assert(err, check.IsNil)

	out, err := ioutil.ReadAll(f)
	c.Assert(err, check.IsNil)
	c.Check(string(out), check.Equals, cfg)

	c.Check(cs.req.Method, check.Equals, "PUT")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
}

func (cs *clientSuite) TestClientGetConfigFromReader(c *check.C) {
	cfg := "config:\n foo:\n  baz: 42"
	cs.rsp = `{"type": "sync", "result": "` + strings.Replace(cfg, "\n", "\\n", -1) + `"}`

	f, err := cs.cli.ConfigFromReader("foo.bar", nil)
	c.Assert(err, check.IsNil)

	out, err := ioutil.ReadAll(f)
	c.Assert(err, check.IsNil)
	c.Check(string(out), check.Equals, cfg)

	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/snaps/foo.bar/config")
}
