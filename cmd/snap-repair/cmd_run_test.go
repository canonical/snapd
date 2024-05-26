// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"encoding/json"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	repair "github.com/snapcore/snapd/cmd/snap-repair"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

func (r *repairSuite) TestNonRoot(c *C) {
	restore := repair.MockOsGetuid(func() int { return 1000 })
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap-repair", "run"}
	mylog.Check(repair.Run())
	c.Assert(err, ErrorMatches, "must be run as root")
}

func (r *repairSuite) TestOffline(c *C) {
	restore := repair.MockOsGetuid(func() int { return 0 })
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()

	r.freshState(c)

	data := mylog.Check2(json.Marshal(repair.RepairConfig{
		StoreOffline: true,
	}))

	mylog.Check(os.MkdirAll(filepath.Dir(dirs.SnapRepairConfigFile), 0755))

	mylog.Check(osutil.AtomicWriteFile(dirs.SnapRepairConfigFile, data, 0644, 0))


	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap-repair", "run"}
	mylog.Check(repair.Run())

}

func (r *repairSuite) TestRun(c *C) {
	restore := repair.MockOsGetuid(func() int { return 0 })
	defer restore()
	restore = release.MockOnClassic(false)
	defer restore()

	r1 := sysdb.InjectTrusted(r.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{r.repairRootAcctKey})
	defer r2()

	r.freshState(c)

	const script = `#!/bin/sh
echo "happy output"
echo "done" >&$SNAP_REPAIR_STATUS_FD
exit 0
`
	seqRepairs := r.signSeqRepairs(c, []string{makeMockRepair(script)})
	mockServer := makeMockServer(c, &seqRepairs, false)
	defer mockServer.Close()

	repair.MockBaseURL(mockServer.URL)

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap-repair", "run"}
	mylog.Check(repair.Run())
	c.Check(err, IsNil)
	c.Check(r.Stdout(), HasLen, 0)

	c.Check(osutil.FileExists(filepath.Join(dirs.SnapRepairRunDir, "canonical", "1", "r0.done")), Equals, true)
}

func (r *repairSuite) TestRunAlreadyLocked(c *C) {
	mylog.Check(os.MkdirAll(dirs.SnapRunRepairDir, 0700))

	flock := mylog.Check2(osutil.NewFileLock(filepath.Join(dirs.SnapRunRepairDir, "lock")))

	mylog.Check(flock.Lock())

	defer flock.Close()
	mylog. // Close unlocks too
		Check(repair.ParseArgs([]string{"run"}))
	c.Check(err, ErrorMatches, `cannot run, another snap-repair run already executing`)
}
