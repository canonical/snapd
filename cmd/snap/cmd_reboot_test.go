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
	"net/http"
	"strings"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestRebootHelp(c *C) {
	msg := `Usage:
  snap.test reboot [reboot-OPTIONS] <label>

The reboot command reboots the system into a particular mode of the selected
recovery system.

[reboot command options]
      --run        Boot into run mode
      --install    Boot into install mode
      --recover    Boot into recover mode

[reboot command arguments]
  <label>:         The recovery system label
`
	s.testSubCommandHelp(c, "reboot", msg)
}

func (s *SnapSuite) TestRebootHappy(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/systems/20200101")
			c.Check(r.URL.RawQuery, Equals, "")
			fmt.Fprintln(w, `{"type": "sync", "result": {}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"reboot", "--recover", "20200101"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, `Reboot into "20200101" with mode "recover" scheduled.
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestRebootUnhappy(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Fatalf("server should not be hit in this test")
	})

	var tc = []struct {
		args   []string
		errStr string
	}{
		{
			args:   []string{"reboot", "20200101"},
			errStr: "Please specify a mode, see --help",
		},
		{
			args:   []string{"reboot", "--run", "--recover", "20200101"},
			errStr: "Please specify a single mode",
		},
		{
			args:   []string{"reboot", "--unknown-mode", "20200101"},
			errStr: "unknown flag `unknown-mode'",
		},
	}

	for _, t := range tc {
		_, err := snap.Parser(snap.Client()).ParseArgs(t.args)
		c.Check(err, ErrorMatches, t.errStr, Commentf(strings.Join(t.args, " ")))
	}
}

func (s *SnapSuite) TestRebootAPIFail(c *C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Equals, "/v2/systems/20200101")
			c.Check(r.URL.RawQuery, Equals, "")
			w.WriteHeader(404)
			fmt.Fprintln(w, `{"type": "error", "status-code":404, "result": {"message":"requested system does not exist"}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"reboot", "--recover", "20200101"})
	c.Assert(err, ErrorMatches, `cannot reboot into system "20200101": cannot request system action: requested system does not exist`)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
