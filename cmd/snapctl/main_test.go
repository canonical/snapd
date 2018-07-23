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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/snapcore/snapd/client"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type snapctlSuite struct {
	server            *httptest.Server
	oldArgs           []string
	expectedContextID string
	expectedArgs      []string
}

var _ = Suite(&snapctlSuite{})

func (s *snapctlSuite) SetUpTest(c *C) {
	os.Setenv("SNAP_COOKIE", "snap-context-test")
	n := 0
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Assert(r.Method, Equals, "POST")
			c.Assert(r.URL.Path, Equals, "/v2/snapctl")
			c.Assert(r.Header.Get("Authorization"), Equals, "")

			var snapctlOptions client.SnapCtlOptions
			decoder := json.NewDecoder(r.Body)
			c.Assert(decoder.Decode(&snapctlOptions), IsNil)
			c.Assert(snapctlOptions.ContextID, Equals, s.expectedContextID)
			c.Assert(snapctlOptions.Args, DeepEquals, s.expectedArgs)

			fmt.Fprintln(w, `{"type": "sync", "result": {"stdout": "test stdout", "stderr": "test stderr"}}`)
		default:
			c.Fatalf("expected to get 1 request, now on %d", n+1)
		}

		n++
	}))
	clientConfig.BaseURL = s.server.URL
	s.oldArgs = os.Args
	os.Args = []string{"snapctl"}
	s.expectedContextID = "snap-context-test"
	s.expectedArgs = []string{}

	fakeAuthPath := filepath.Join(c.MkDir(), "auth.json")
	os.Setenv("SNAPD_AUTH_DATA_FILENAME", fakeAuthPath)
	err := ioutil.WriteFile(fakeAuthPath, []byte(`{"macaroon":"user-macaroon"}`), 0644)
	c.Assert(err, IsNil)
}

func (s *snapctlSuite) TearDownTest(c *C) {
	os.Unsetenv("SNAP_COOKIE")
	os.Unsetenv("SNAPD_AUTH_DATA_FILENAME")
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

func (s *snapctlSuite) TestSnapctlHelp(c *C) {
	os.Unsetenv("SNAP_COOKIE")
	s.expectedContextID = ""

	os.Args = []string{"snapctl", "-h"}
	s.expectedArgs = []string{"-h"}

	_, _, err := run()
	c.Check(err, IsNil)
}
