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

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/snapcore/snapd/overlord/hookstate"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type snapctlSuite struct {
	server          *httptest.Server
	oldArgs         []string
	expectedContext string
	expectedArgs    []string
}

var _ = Suite(&snapctlSuite{})

func (s *snapctlSuite) SetUpTest(c *C) {
	os.Setenv("SNAP_CONTEXT", "snap-context-test")
	n := 0
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Assert(r.Method, Equals, "POST")
			c.Assert(r.URL.Path, Equals, "/v2/snapctl")

			var toolRequest hookstate.ToolRequest
			decoder := json.NewDecoder(r.Body)
			c.Assert(decoder.Decode(&toolRequest), IsNil)
			c.Assert(toolRequest.Context, Equals, s.expectedContext)
			c.Assert(toolRequest.Args, DeepEquals, s.expectedArgs)

			fmt.Fprintln(w, `{"type": "sync", "result": {"stdout": "test stdout", "stderr": "test stderr"}}`)
		default:
			c.Fatalf("expected to get 1 request, now on %d", n+1)
		}

		n++
	}))
	clientConfig.BaseURL = s.server.URL
	s.oldArgs = os.Args
	os.Args = []string{"snapctl"}
	s.expectedContext = "snap-context-test"
	s.expectedArgs = []string{}
}

func (s *snapctlSuite) TearDownTest(c *C) {
	os.Unsetenv("SNAP_CONTEXT")
	clientConfig.BaseURL = ""
	s.server.Close()
	os.Args = s.oldArgs
}

func (s *snapctlSuite) TestSnapctl(c *C) {
	stdout, stderr, err := run()
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "test stdout")
	c.Check(string(stderr), Equals, "test stderr")
}

func (s *snapctlSuite) TestSnapctlWithArgs(c *C) {
	os.Args = []string{"snapctl", "foo", "--bar"}

	s.expectedArgs = []string{"foo", "--bar"}
	stdout, stderr, err := run()
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "test stdout")
	c.Check(string(stderr), Equals, "test stderr")
}

func (s *snapctlSuite) TestSnapctlWithoutContextShouldError(c *C) {
	os.Unsetenv("SNAP_CONTEXT")
	_, _, err := run()
	c.Check(err, ErrorMatches, ".*requires SNAP_CONTEXT.*")
}
