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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/testutil"
)

var snapshotsTests = []getCmdArgs{{
	args:  "restore x",
	error: `invalid argument for set id: expected a non-negative integer argument \(see 'snap help saved'\)`,
}, {
	args:  "saved --id=x",
	error: `invalid argument for set id: expected a non-negative integer argument \(see 'snap help saved'\)`,
}, {
	args:   "saved --id=3",
	stdout: "Set  Snap  Age    Version  Rev   Size    Notes\n3    htop  .*  2        1168      1B  auto\n",
}, {
	args:   "saved",
	stdout: "Set  Snap  Age    Version  Rev   Size    Notes\n1    htop  .*  2        1168      1B  -\n",
}, {
	args:  "forget x",
	error: `invalid argument for set id: expected a non-negative integer argument \(see 'snap help saved'\)`,
}, {
	args:  "check-snapshot x",
	error: `invalid argument for set id: expected a non-negative integer argument \(see 'snap help saved'\)`,
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
			c.Check(s.Stderr(), testutil.EqualsWrapped, test.stderr)
			c.Check(s.Stdout(), testutil.MatchesWrapped, test.stdout)
		}
	}
}

func (s *SnapSuite) mockSnapshotsServer(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/snapshots":
			if r.Method == "GET" {
				// simulate a 1-month old snapshot
				snapshotTime := time.Now().AddDate(0, -1, 0).Format(time.RFC3339)
				if r.URL.Query().Get("set") == "3" {
					fmt.Fprintf(w, `{"type":"sync","status-code":200,"status":"OK","result":[{"id":3,"snapshots":[{"set":3,"time":%q,"snap":"htop","revision":"1168","snap-id":"Z","auto":true,"epoch":{"read":[0],"write":[0]},"summary":"","version":"2","sha3-384":{"archive.tgz":""},"size":1}]}]}`, snapshotTime)
					return
				}
				fmt.Fprintf(w, `{"type":"sync","status-code":200,"status":"OK","result":[{"id":1,"snapshots":[{"set":1,"time":%q,"snap":"htop","revision":"1168","snap-id":"Z","epoch":{"read":[0],"write":[0]},"summary":"","version":"2","sha3-384":{"archive.tgz":""},"size":1}]}]}`, snapshotTime)
			} else {
				w.WriteHeader(202)
				fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "9"}`)
			}
		case "/v2/changes/9":
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {}}}`)
		default:
			c.Errorf("unexpected path %q", r.URL.Path)
		}
	})
}
