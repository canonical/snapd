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
	"io/ioutil"
	"net/http"
	"strings"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestRebootHelp(c *C) {
	msg := `Usage:
  snap.test reboot [reboot-OPTIONS] [<label>]

The reboot command reboots the system into a particular mode of the selected
recovery system.

When called without a system label and without a mode it will just
trigger a regular reboot.

When called without a system label but with a mode it will use the
current system to enter the given mode.

Note that "recover", "factory-reset" and "run" modes are only available for the
current system.

"--reseal" will force resealing keys on the system before
rebooting. This is only available for the current system.

[reboot command options]
      --no-wait          Do not wait for the operation to finish but just print
                         the change id.
      --run              Boot into run mode
      --install          Boot into install mode
      --recover          Boot into recover mode
      --factory-reset    Boot into factory-reset mode
      --reseal           Reseal the keys to the device before rebooting

[reboot command arguments]
  <label>:               The recovery system label
`
	s.testSubCommandHelp(c, "reboot", msg)
}

func (s *SnapSuite) TestRebootHappy(c *C) {

	type request struct {
		method           string
		expectedEndpoint string
		expectedJSON     string
		status           int
		response         string
	}
	for _, tc := range []struct {
		cmdline     []string
		expectedMsg string
		expectedErr string
		requests    []request
	}{
		{
			cmdline:     []string{"reboot"},
			expectedMsg: `Reboot` + "\n",
			requests: []request{{
				method:           "POST",
				expectedEndpoint: "/v2/systems",
				expectedJSON:     `{"action":"reboot","mode":""}` + "\n",
				status:           200,
				response:         `{"type": "sync", "result": {}}`,
			}},
		},
		{
			cmdline:     []string{"reboot", "--recover"},
			expectedMsg: `Reboot into "recover" mode.` + "\n",
			requests: []request{{
				method:           "POST",
				expectedEndpoint: "/v2/systems",
				expectedJSON:     `{"action":"reboot","mode":"recover"}` + "\n",
				status:           200,
				response:         `{"type": "sync", "result": {}}`,
			}},
		},
		{
			cmdline:     []string{"reboot", "20200101"},
			expectedMsg: `Reboot into "20200101".` + "\n",
			requests: []request{{
				method:           "POST",
				expectedEndpoint: "/v2/systems/20200101",
				expectedJSON:     `{"action":"reboot","mode":""}` + "\n",
				status:           200,
				response:         `{"type": "sync", "result": {}}`,
			}},
		},
		{
			cmdline:     []string{"reboot", "--recover", "20200101"},
			expectedMsg: `Reboot into "20200101" "recover" mode.` + "\n",
			requests: []request{{
				method:           "POST",
				expectedEndpoint: "/v2/systems/20200101",
				expectedJSON:     `{"action":"reboot","mode":"recover"}` + "\n",
				status:           200,
				response:         `{"type": "sync", "result": {}}`,
			}},
		},
		{
			cmdline:     []string{"reboot", "--factory-reset", "20200101"},
			expectedMsg: `Reboot into "20200101" "factory-reset" mode.` + "\n",
			requests: []request{{
				method:           "POST",
				expectedEndpoint: "/v2/systems/20200101",
				expectedJSON:     `{"action":"reboot","mode":"factory-reset"}` + "\n",
				status:           200,
				response:         `{"type": "sync", "result": {}}`,
			}},
		},
		{
			cmdline:     []string{"reboot", "--reseal", "--no-wait"},
			expectedMsg: `42` + "\n",
			requests: []request{{
				method:           "POST",
				expectedEndpoint: "/v2/system-reseal",
				expectedJSON:     `{"reboot":true}`,
				status:           202,
				response:         `{"type": "async", "change": "42", "status-code": 202}`,
			}},
		},
		{
			cmdline:     []string{"reboot", "--reseal"},
			expectedMsg: ``,
			requests: []request{{
				method:           "POST",
				expectedEndpoint: "/v2/system-reseal",
				expectedJSON:     `{"reboot":true}`,
				status:           202,
				response:         `{"type": "async", "change": "42", "status-code": 202}`,
			}, {
				method:           "GET",
				expectedEndpoint: "/v2/changes/42",
				status:           200,
				response:         `{"type": "sync", "result": {"ready": true, "status": "Done"}}` + "\n",
			}},
		},
		{
			cmdline:     []string{"reboot", "--reseal"},
			expectedMsg: ``,
			expectedErr: `(?s).*some error.*`,
			requests: []request{{
				method:           "POST",
				expectedEndpoint: "/v2/system-reseal",
				expectedJSON:     `{"reboot":true}`,
				status:           202,
				response:         `{"type": "async", "change": "42", "status-code": 202}`,
			}, {
				method:           "GET",
				expectedEndpoint: "/v2/changes/42",
				status:           400,
				response:         `{"type": "error", "result": {"message": "some error"}, "status-code": 400}`,
			}},
		},
	} {

		n := 0
		s.ResetStdStreams()

		s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
			if n > len(tc.requests) {
				c.Fatalf("expected to get %d requests, now on %d", len(tc.requests), n+1)
			}
			e := tc.requests[n]
			n++
			c.Check(r.Method, Equals, e.method)
			c.Check(r.URL.Path, Equals, e.expectedEndpoint, Commentf("%v", tc.cmdline))
			c.Check(r.URL.RawQuery, Equals, "")
			body, err := ioutil.ReadAll(r.Body)
			c.Check(err, IsNil)
			c.Check(string(body), Equals, e.expectedJSON)
			w.WriteHeader(e.status)
			fmt.Fprintln(w, e.response)
		})

		// The server side will work out if the request is valid
		rest, err := snap.Parser(snap.Client()).ParseArgs(tc.cmdline)
		if tc.expectedErr != "" {
			c.Assert(err, ErrorMatches, tc.expectedErr)
		} else {
			c.Assert(err, IsNil)
			c.Assert(rest, DeepEquals, []string{})
		}
		c.Check(s.Stdout(), Equals, tc.expectedMsg, Commentf("%v", tc.cmdline))
		c.Check(s.Stderr(), Equals, "")
		c.Check(n, Equals, len(tc.requests))
	}
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
	c.Assert(err, ErrorMatches, `cannot request system reboot into "20200101": requested system does not exist`)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
