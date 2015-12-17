// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!excludereboots

/*
 * Copyright (C) 2015 Canonical Ltd
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

package tests

import (
	"io/ioutil"
	"path"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/partition"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&updateSuite{})

type updateSuite struct {
	common.SnappySuite
}

func (s *updateSuite) assertBootDirContents(c *check.C) {
	system, err := partition.BootSystem()
	c.Assert(err, check.IsNil, check.Commentf("Error getting the boot system: %s", err))
	current, err := partition.CurrentPartition()
	c.Assert(err, check.IsNil, check.Commentf("Error getting the current partition: %s", err))
	files, err := ioutil.ReadDir(
		path.Join(partition.BootDir(system), partition.OtherPartition(current)))
	c.Assert(err, check.IsNil, check.Commentf("Error reading the other partition boot dir: %s", err))

	expectedFileNames := []string{"hardware.yaml", "initrd.img", "vmlinuz"}
	if system == "uboot" {
		expectedFileNames = append([]string{"dtbs"}, expectedFileNames...)
	}

	fileNames := []string{}
	for _, f := range files {
		fileNames = append(fileNames, f.Name())
	}
	c.Assert(fileNames, check.DeepEquals, expectedFileNames,
		check.Commentf("Wrong files in the other partition boot dir"))
}

// Test that the update to the same release and channel must install a newer
// version. If there is no update available, the channel version will be
// modified to fake an update. If there is a version available, the image will
// be up-to-date after running this test.
func (s *updateSuite) TestUpdateToSameReleaseAndChannel(c *check.C) {
	if common.BeforeReboot() {
		updateOutput := common.CallFakeUpdate(c)
		expected := "(?ms)" +
			".*" +
			"^Reboot to use ubuntu-core version .*\\.\n"
		c.Assert(updateOutput, check.Matches, expected)
		s.assertBootDirContents(c)
		common.Reboot(c)
	} else if common.AfterReboot(c) {
		common.RemoveRebootMark(c)
		currentVersion := common.GetCurrentUbuntuCoreVersion(c)
		c.Assert(currentVersion > common.GetSavedVersion(c), check.Equals, true,
			check.Commentf("Rebooted to the wrong version: %d", currentVersion))
	}
}
