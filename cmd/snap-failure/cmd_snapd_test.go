// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	failure "github.com/snapcore/snapd/cmd/snap-failure"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func (r *failureSuite) TestRun(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)
}

func writeSeqFile(c *C, name string, current snap.Revision, seq []*snap.SideInfo) {
	seqPath := filepath.Join(dirs.SnapSeqDir, name+".json")

	err := os.MkdirAll(dirs.SnapSeqDir, 0755)
	c.Assert(err, IsNil)

	b, err := json.Marshal(&struct {
		Sequence []*snap.SideInfo `json:"sequence"`
		Current  string           `json:"current"`
	}{
		Sequence: seq,
		Current:  current.String(),
	})
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(seqPath, b, 0644)
	c.Assert(err, IsNil)
}

func (r *failureSuite) TestCallPrevSnapdFromSnap(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(99)},
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	// mock snapd command from 'previous' revision
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"),
		`test "$SNAPD_REVERT_TO_REV" = "100"`)
	defer snapdCmd.Restore()

	systemctlCmd := testutil.MockCommand(c, "systemctl", "")
	defer systemctlCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
	})
	c.Check(systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "restart", "snapd.socket"},
	})
}

func (r *failureSuite) TestCallPrevSnapdFromCore(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// only one entry in sequence
	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(123)},
	})

	// mock snapd in the core snap
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd"),
		`test "$SNAPD_REVERT_TO_REV" = "0"`)
	defer snapdCmd.Restore()

	systemctlCmd := testutil.MockCommand(c, "systemctl", "")
	defer systemctlCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
	})
	c.Check(systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "restart", "snapd.socket"},
	})
}

func (r *failureSuite) TestCallPrevSnapdFail(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	// mock snapd in the core snap
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"),
		`exit 2`)
	defer snapdCmd.Restore()

	systemctlCmd := testutil.MockCommand(c, "systemctl", "")
	defer systemctlCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, ErrorMatches, "snapd failed: exit status 2")
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
	})
	c.Check(systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
	})
}

func (r *failureSuite) TestGarbageSeq(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	seqPath := filepath.Join(dirs.SnapSeqDir, "snapd.json")
	err := os.MkdirAll(dirs.SnapSeqDir, 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(seqPath, []byte("this is garbage"), 0644)
	c.Assert(err, IsNil)

	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"),
		`exit 99`)
	defer snapdCmd.Restore()

	systemctlCmd := testutil.MockCommand(c, "systemctl", "exit 98")
	defer systemctlCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, ErrorMatches, `cannot parse "snapd.json" sequence file: invalid .*`)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), HasLen, 0)
	c.Check(systemctlCmd.Calls(), HasLen, 0)
}

func (r *failureSuite) TestBadSeq(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(100)},
		// current not in sequence
	})

	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"), "")
	defer snapdCmd.Restore()
	systemctlCmd := testutil.MockCommand(c, "systemctl", "")
	defer systemctlCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, ErrorMatches, "internal error: current 123 not found in sequence: .*Revision:100.*")
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), HasLen, 0)
	c.Check(systemctlCmd.Calls(), HasLen, 0)
}
