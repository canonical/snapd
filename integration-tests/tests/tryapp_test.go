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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/integration-tests/testutils/build"
	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"
	"github.com/snapcore/snapd/integration-tests/testutils/data"
	"github.com/snapcore/snapd/testutil"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&trySuite{})

type trySuite struct {
	common.SnappySuite
}

func (s *trySuite) TestTryBasicBinaries(c *check.C) {
	tryPath, err := filepath.Abs(filepath.Join(data.BaseSnapPath, data.BasicBinariesSnapName))
	c.Assert(err, check.IsNil)

	tryOutput := cli.ExecCommand(c, "sudo", "snap", "try", tryPath)
	defer common.RemoveSnap(c, data.BasicBinariesSnapName)

	expected := "(?ms)" +
		".*" +
		"Name +Version +Rev +Developer +Notes\n" +
		"basic-binaries +.*try"
	c.Check(tryOutput, check.Matches, expected)

	// can run commands from the snap-try binary
	cli.ExecCommand(c, "basic-binaries.success")

	// commands can read stuff in their own dir
	output := cli.ExecCommand(c, "basic-binaries.cat")
	c.Check(output, testutil.Contains, "I output myself")
}

func (s *trySuite) TestTryConfinmentDenies(c *check.C) {
	tryPath, err := filepath.Abs(filepath.Join(data.BaseSnapPath, data.DevKmsg))
	c.Assert(err, check.IsNil)

	cli.ExecCommand(c, "sudo", "snap", "try", tryPath)
	defer common.RemoveSnap(c, data.DevKmsg)

	// confinment works in try mode:
	//  dev-kmsg.reader can not read /dev/kmsg out of the box
	output, err := cli.ExecCommandErr("dev-kmsg.reader")
	c.Check(err, check.NotNil)
	c.Check(output, check.Matches, `(?ms)dd: failed to open '/dev/kmsg': Permission denied`)
}

func (s *trySuite) TestTryConfinmentDevModeAllows(c *check.C) {
	tryPath, err := filepath.Abs(filepath.Join(data.BaseSnapPath, data.DevKmsg))
	c.Assert(err, check.IsNil)

	cli.ExecCommand(c, "sudo", "snap", "try", "--devmode", tryPath)
	defer common.RemoveSnap(c, data.DevKmsg)

	// dev-mode disables confinement and we can read /dev/kmsg
	output, err := cli.ExecCommandErr("dev-kmsg.reader")
	c.Check(err, check.IsNil)
	c.Check(output, check.Not(check.HasLen), 0)
}

func (s *trySuite) TestTryConfinmentAllows(c *check.C) {
	// provide a server for the test
	snapPath, err := build.LocalSnap(c, data.NetworkBindConsumerSnapName)
	c.Assert(err, check.IsNil)
	defer os.Remove(snapPath)
	common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.NetworkBindConsumerSnapName)

	// "try" client confinment (network auto-connects)
	tryPath, err := filepath.Abs(filepath.Join(data.BaseSnapPath, data.NetworkConsumerSnapName))
	c.Assert(err, check.IsNil)

	cli.ExecCommand(c, "sudo", "snap", "try", tryPath)
	defer common.RemoveSnap(c, data.NetworkConsumerSnapName)

	// confinment works in try mode:
	providerURL := "http://127.0.0.1:8081"
	output := cli.ExecCommand(c, "network-consumer", providerURL)
	c.Assert(output, check.Equals, "ok\n")
}
