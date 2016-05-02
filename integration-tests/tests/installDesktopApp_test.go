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

package tests

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/testutil"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&installDesktopAppSuite{})

type installDesktopAppSuite struct {
	common.SnappySuite
}

func (s *installDesktopAppSuite) TestInstallsDesktopFile(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicDesktopSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicDesktopSnapName)

	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapDesktopFilesDir, "basic-desktop_echo.desktop"))
	c.Assert(err, check.IsNil)
	c.Assert(string(content), testutil.Contains, `[Desktop Entry]
Name=Echo
Comment=It echos stuff
Exec=/snap/bin/basic-desktop.echo
`)
}
