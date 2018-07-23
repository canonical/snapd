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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	repair "github.com/snapcore/snapd/cmd/snap-repair"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

func (r *repairSuite) TestRun(c *C) {
	defer release.MockOnClassic(false)()

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
	err := repair.Run()
	c.Check(err, IsNil)
	c.Check(r.Stdout(), HasLen, 0)

	c.Check(osutil.FileExists(filepath.Join(dirs.SnapRepairRunDir, "canonical", "1", "r0.done")), Equals, true)
}

func (r *repairSuite) TestRunAlreadyLocked(c *C) {
	err := os.MkdirAll(dirs.SnapRunRepairDir, 0700)
	c.Assert(err, IsNil)
	flock, err := osutil.NewFileLock(filepath.Join(dirs.SnapRunRepairDir, "lock"))
	c.Assert(err, IsNil)
	err = flock.Lock()
	c.Assert(err, IsNil)
	defer flock.Unlock()

	err = repair.ParseArgs([]string{"run"})
	c.Check(err, ErrorMatches, `cannot run, another snap-repair run already executing`)
}
