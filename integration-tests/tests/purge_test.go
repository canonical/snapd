// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&purgeSuite{})

const (
	snap             = data.BasicSnapName + ".sideload"
	baseSnapDataPath = "/var/lib/apps"
)

var snapDataPath = filepath.Join(baseSnapDataPath, snap, "current")

type purgeSuite struct {
	common.SnappySuite
}

func (s *purgeSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	snapPath, err := build.LocalSnap(c, data.BasicSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
}

func (s *purgeSuite) TestPurgeRemovesDataFromRemovedPackage(c *check.C) {
	common.RemoveSnap(c, data.BasicSnapName)

	versionSnapDataPath, err := getVersionSnapDataPath(snap)
	c.Assert(err, check.IsNil)

	_, err = os.Stat(versionSnapDataPath)
	c.Assert(err, check.IsNil)

	_, err = cli.ExecCommandErr("sudo", "snappy", "purge", snap)
	c.Assert(err, check.IsNil)

	_, err = os.Stat(versionSnapDataPath)
	c.Assert(os.IsNotExist(err), check.Equals, true)
}

func (s *purgeSuite) TestPurgeReturnsErrorForNotRemovedPackage(c *check.C) {
	defer common.RemoveSnap(c, data.BasicSnapName)

	_, err := cli.ExecCommandErr("sudo", "snappy", "purge", snap)
	c.Assert(err, check.NotNil)

	_, err = os.Stat(snapDataPath)
	c.Assert(err, check.IsNil)
}

func (s *purgeSuite) TestPurgeHonoursInstalledFlagForNotRemovedPackage(c *check.C) {
	defer common.RemoveSnap(c, data.BasicSnapName)

	dataFile := filepath.Join(snapDataPath, "data")
	_, err := cli.ExecCommandErr("sudo", "touch", dataFile)
	c.Assert(err, check.IsNil)

	_, err = cli.ExecCommandErr("sudo", "snappy", "purge", snap, "--installed")
	c.Assert(err, check.IsNil)

	_, err = os.Stat(dataFile)
	c.Assert(os.IsNotExist(err), check.Equals, true, check.Commentf("Error %v instead of os.IsNotExist", err))

	_, err = os.Stat(snapDataPath)
	c.Assert(err, check.IsNil)
}

func (s *purgeSuite) TestPurgeRemovesDataForDeactivatedNotRemovedPackage(c *check.C) {
	defer common.RemoveSnap(c, data.BasicSnapName)
	_, err := cli.ExecCommandErr("sudo", "snappy", "deactivate", snap)
	c.Assert(err, check.IsNil)

	_, err = cli.ExecCommandErr("sudo", "snappy", "purge", snap)
	c.Assert(err, check.IsNil)

	_, err = os.Stat(snapDataPath)
	c.Assert(os.IsNotExist(err), check.Equals, true)
}

// getVersionSnapDataPath returns the $SNAP_APP_DATA_PATH for a given snap
// assuming that there's only one version installed
func getVersionSnapDataPath(snap string) (versionPath string, err error) {
	snapFullPath := filepath.Join(baseSnapDataPath, snap)

	list, err := ioutil.ReadDir(snapFullPath)
	if list[0].Name() != "current" {
		versionPath = filepath.Join(snapFullPath, list[0].Name())
	}

	return versionPath, err
}
