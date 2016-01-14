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

package updates

import (
	"fmt"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/store"
)

// ChangeFakeUpdateSnap is the type of the functions used to modify a snap before it is served as
// a fake update.
type ChangeFakeUpdateSnap func(snapPath string) error

// NoOp leaves the snap unchanged.
func NoOp(snapPath string) error {
	return nil
}

// CallFakeUpdate calls snappy update after faking a new version available for the specified snap.
// The fake is made copying the currently installed snap.
// changeFunc can be used to modify the snap before it is built and served.
func CallFakeUpdate(c *check.C, snap string, changeFunc ChangeFakeUpdateSnap) string {
	c.Log("Preparing fake and calling update.")

	blobDir := c.MkDir()
	fakeStore := store.NewStore(blobDir)
	fakeStore.Start()
	defer fakeStore.Stop()

	makeFakeUpdateForSnap(c, snap, blobDir, changeFunc)

	return cli.ExecCommand(c, "sudo", fmt.Sprintf("SNAPPY_FORCE_CPI_URL=%s", fakeStore.URL()), "snappy", "update")
}

// CallFakeOSUpdate calls snappy update after faking a new version available for the OS snap.
func CallFakeOSUpdate(c *check.C) string {
	currentVersion := common.GetCurrentUbuntuCoreVersion(c)
	common.SetSavedVersion(c, currentVersion)

	return CallFakeUpdate(c, "ubuntu-core.canonical", NoOp)
}

func makeFakeUpdateForSnap(c *check.C, snap, targetDir string, changeFunc ChangeFakeUpdateSnap) error {
	// make a fake update snap
	fakeUpdateDir := c.MkDir()
	defer cli.ExecCommand(c, "sudo", "rm", "-rf", fakeUpdateDir)

	copySnap(c, snap, fakeUpdateDir)

	// fake new version
	cli.ExecCommand(c, "sudo", "sed", "-i", `s/version:\(.*\)/version:\1+fake1/`, filepath.Join(fakeUpdateDir, "meta/package.yaml"))

	if err := changeFunc(fakeUpdateDir); err != nil {
		return err
	}
	buildSnap(c, fakeUpdateDir, targetDir)
	return nil
}

func copySnap(c *check.C, snap, targetDir string) {
	sourceDir := filepath.Join("/snaps", snap, "current")
	files, err := filepath.Glob(filepath.Join(sourceDir, "*"))
	c.Assert(err, check.IsNil)
	for _, m := range files {
		cli.ExecCommand(c, "sudo", "cp", "-a", m, targetDir)
	}
}

func buildSnap(c *check.C, snapDir, targetDir string) {
	helpers.ChDir(targetDir, func() error {
		cli.ExecCommand(c, "sudo", "snappy", "build", "--squashfs", snapDir)
		return nil
	})
}
