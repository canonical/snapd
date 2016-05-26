// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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

package refresh

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/store"
)

// ChangeFakeUpdateSnap is the type of the functions used to modify a snap before it is served as
// a fake update.
type ChangeFakeUpdateSnap func(snapPath string) error

// NoOp leaves the snap unchanged.
func NoOp(snapPath string) error {
	return nil
}

// CallFakeSnapRefresh calls snappy update after faking a new version available for the specified snap.
// The fake is made copying the currently installed snap.
// changeFunc can be used to modify the snap before it is built and served.
func CallFakeSnapRefreshForSnap(c *check.C, snap string, changeFunc ChangeFakeUpdateSnap, fakeStore *store.Store) string {
	c.Log("Preparing fake single snap and calling update.")

	blobDir := fakeStore.SnapsDir()
	MakeFakeRefreshForSnap(c, snap, blobDir, changeFunc)

	cli.ExecCommand(c, "sudo", "snap", "refresh", snap)

	// FIXME: do we want an automatic `snap list` output after
	//        `snap update` (like in the old snappy world)?
	return cli.ExecCommand(c, "snap", "list")
}

func CallFakeSnapRefreshAll(c *check.C, snaps []string, changeFunc ChangeFakeUpdateSnap, fakeStore *store.Store) string {
	c.Log("Preparing fake and calling update.")

	blobDir := fakeStore.SnapsDir()
	for _, snap := range snaps {
		MakeFakeRefreshForSnap(c, snap, blobDir, changeFunc)
	}

	return cli.ExecCommand(c, "sudo", "snap", "refresh")
}

func MakeFakeRefreshForSnap(c *check.C, snap, targetDir string, changeFunc ChangeFakeUpdateSnap) error {

	// make a fake update snap in /var/tmp (which is not a tempfs)
	fakeUpdateDir, err := ioutil.TempDir("/var/tmp", "snap-build-")
	c.Assert(err, check.IsNil)
	// ensure the "." of the squashfs has sane owner/permissions
	cli.ExecCommand(c, "sudo", "chown", "root:root", fakeUpdateDir)
	cli.ExecCommand(c, "sudo", "chmod", "0755", fakeUpdateDir)
	defer cli.ExecCommand(c, "sudo", "rm", "-rf", fakeUpdateDir)

	copySnap(c, snap, fakeUpdateDir)

	// fake new version
	cli.ExecCommand(c, "sudo", "sed", "-i", `s/version:\(.*\)/version:\1+fake1/`, filepath.Join(fakeUpdateDir, "meta/snap.yaml"))

	if err := changeFunc(fakeUpdateDir); err != nil {
		return err
	}
	buildSnap(c, fakeUpdateDir, targetDir)
	return nil
}

func copySnap(c *check.C, snap, targetDir string) {
	// check for sideloaded snaps
	// XXX: simplify this down to consider only the name (and not origin)
	// in the directory once everything is moved to that
	baseDir := filepath.Join(dirs.SnapSnapsDir, snap)
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		snapName := strings.Split(snap, ".")[0]
		baseDir = filepath.Join(dirs.SnapSnapsDir, snapName)
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			baseDir = filepath.Join(dirs.SnapSnapsDir, snapName+".sideload")
			_, err = os.Stat(baseDir)
			c.Assert(err, check.IsNil,
				check.Commentf("%s not found from it's original source not sideloaded", snap))
		}
	}
	sourceDir := filepath.Join(baseDir, "current")
	files, err := filepath.Glob(filepath.Join(sourceDir, "*"))
	c.Assert(err, check.IsNil)
	for _, m := range files {
		cli.ExecCommand(c, "sudo", "cp", "-a", m, targetDir)
	}
}

func buildSnap(c *check.C, snapDir, targetDir string) {
	// build in /var/tmp (which is not a tempfs)
	cli.ExecCommand(c, "sudo", "TMPDIR=/var/tmp", "snapbuild", snapDir, targetDir)
}
