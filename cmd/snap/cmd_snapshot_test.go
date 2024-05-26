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
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	main "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/strutil/quantity"
	"github.com/snapcore/snapd/testutil"
)

var snapshotsTests = []getCmdArgs{{
	args:  "restore x",
	error: `invalid argument for snapshot set id: expected a non-negative integer argument \(see 'snap help saved'\)`,
}, {
	args:  "saved --id=x",
	error: `invalid argument for snapshot set id: expected a non-negative integer argument \(see 'snap help saved'\)`,
}, {
	args:   "saved --id=3",
	stdout: "Set  Snap  Age    Version  Rev   Size    Notes\n3    htop  .*  2        1168      1B  auto\n",
}, {
	args:   "saved",
	stdout: "Set  Snap  Age    Version  Rev   Size    Notes\n1    htop  .*  2        1168      1B  -\n",
}, {
	args:  "forget x",
	error: `invalid argument for snapshot set id: expected a non-negative integer argument \(see 'snap help saved'\)`,
}, {
	args:  "check-snapshot x",
	error: `invalid argument for snapshot set id: expected a non-negative integer argument \(see 'snap help saved'\)`,
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
}, {
	args:  "export-snapshot x snapshot-export.snapshot",
	error: `invalid argument for snapshot set id: expected a non-negative integer argument \(see 'snap help saved'\)`,
}, {
	args:  "export-snapshot 1",
	error: "the required argument `<filename>` was not provided",
}}

func (s *SnapSuite) TestSnapSnaphotsTest(c *C) {
	s.mockSnapshotsServer(c)

	restore := main.MockIsStdinTTY(true)
	defer restore()

	for _, test := range snapshotsTests {
		s.stdout.Truncate(0)
		s.stderr.Truncate(0)

		c.Logf("Test: %s", test.args)

		_ := mylog.Check2(main.Parser(main.Client()).ParseArgs(strings.Fields(test.args)))
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(s.Stderr(), testutil.EqualsWrapped, test.stderr)
			c.Check(s.Stdout(), testutil.MatchesWrapped, test.stdout)
		}
		c.Check("snapshot-export.snapshot", testutil.FileAbsent)
		c.Check("snapshot-export.snapshot.part", testutil.FileAbsent)
	}
}

func (s *SnapSuite) TestSnapshotExportHappy(c *C) {
	s.mockSnapshotsServer(c)

	exportedSnapshotPath := filepath.Join(c.MkDir(), "export-snapshot.snapshot")
	_ := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"export-snapshot", "1", exportedSnapshotPath}))
	c.Check(err, IsNil)
	c.Check(s.Stderr(), testutil.EqualsWrapped, "")
	c.Check(s.Stdout(), testutil.MatchesWrapped, `Exported snapshot #1 into ".*/export-snapshot.snapshot"`)
	c.Check(exportedSnapshotPath, testutil.FileEquals, "Hello World!")
	c.Check(exportedSnapshotPath+".part", testutil.FileAbsent)
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
			}
			if r.Method == "POST" {
				if r.Header.Get("Content-Type") == client.SnapshotExportMediaType {
					fmt.Fprintln(w, `{"type": "sync", "result": {"set-id": 42, "snaps": ["htop"]}}`)
				} else {

					w.WriteHeader(202)
					fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "9"}`)
				}
			}
		case "/v2/changes/9":
			fmt.Fprintln(w, `{"type": "sync", "result": {"ready": true, "status": "Done", "data": {}}}`)
		case "/v2/snapshots/1/export":
			w.Header().Set("Content-Type", client.SnapshotExportMediaType)
			fmt.Fprint(w, "Hello World!")
		default:
			c.Errorf("unexpected path %q", r.URL.Path)
		}
	})
}

func (s *SnapSuite) TestSnapshotImportHappy(c *C) {
	// mockSnapshotServer will return set-id 42 and three snaps for all
	// import calls
	s.mockSnapshotsServer(c)

	// time may be crossing DST change, so the age value should not be
	// hardcoded, otherwise we'll see failures for 2 montsh during the year
	expectedAge := time.Since(time.Now().AddDate(0, -1, 0))
	ageStr := quantity.FormatDuration(expectedAge.Seconds())

	exportedSnapshotPath := filepath.Join(c.MkDir(), "mocked-snapshot.snapshot")
	os.WriteFile(exportedSnapshotPath, []byte("this is really snapshot zip file data"), 0644)

	_ := mylog.Check2(main.Parser(main.Client()).ParseArgs([]string{"import-snapshot", exportedSnapshotPath}))
	c.Check(err, IsNil)
	c.Check(s.Stderr(), testutil.EqualsWrapped, "")
	c.Check(s.Stdout(), testutil.MatchesWrapped, fmt.Sprintf(`Imported snapshot as #42
Set  Snap  Age    Version  Rev   Size    Notes
1    htop  %-6s 2        1168      1B  -
`, ageStr))
}
