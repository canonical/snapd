// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	"github.com/snapcore/snapd/cmd/snap"
)

type snapshotCmdArgs struct {
	args, stdout, stderr, error string
}

var snapshotsTests = []getCmdArgs{{
	args:  "restore x",
	error: "invalid argument for set id: expected a non-negative integer argument",
}, {
	args:  "saved --id=x",
	error: "invalid argument for set id: expected a non-negative integer argument",
}, {
	args:  "forget x",
	error: "invalid argument for set id: expected a non-negative integer argument",
}, {
	args:  "check-snapshot x",
	error: "invalid argument for set id: expected a non-negative integer argument",
}, {
	args:   "restore 1",
	stdout: "Restored snapshot #1.\n",
}, {
	args:   "forget 2",
	stdout: "Snapshot #2 forgotten.\n",
}, {
	args:   "forget 2 snap1 snap2",
	stdout: "Snapshot #2 of snaps \"snap1\", \"snap2\" forgotten.\n",
}, {
	args:   "check-snapshot 4",
	stdout: "Snapshot #4 verified successfully.\n",
}, {
	args:   "check-snapshot 4 snap1 snap2",
	stdout: "Snapshot #4 of snaps \"snap1\", \"snap2\" verified successfully.\n",
}}

func (s *SnapSuite) TestSnapSnaphotsTest(c *C) {
	s.mockSnapshotsServer(c)

	restore := main.MockIsStdinTTY(true)
	defer restore()

	for _, test := range snapshotsTests {
		s.stdout.Truncate(0)
		s.stderr.Truncate(0)

		c.Logf("Test: %s", test.args)

		_, err := main.Parser(main.Client()).ParseArgs(strings.Fields(test.args))
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(s.Stderr(), Equals, test.stderr)
			c.Check(s.Stdout(), Equals, test.stdout)
		}
	}
}

func (s *SnapSuite) mockSnapshotsServer(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/snapshots":
			fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "9"}`)
		case "/v2/changes/9":
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {}}}`)
		default:
			c.Errorf("unexpected path %q", r.URL.Path)
		}
	})
}
