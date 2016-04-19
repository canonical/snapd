// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&networkInterfaceSuite{})

const (
	connectedPattern = "(?msi)" +
		"slot +plug\n" +
		".*" +
		"^:network +network-consumer\n" +
		".*"
	disconnectedPattern = "(?msi)" +
		"slot +plug\n" +
		".*" +
		"^:network +-\n" +
		".*" +
		"^- +network-consumer:network\n" +
		".*"
	networkAccessibleOutput = "ok\n"
)

type networkInterfaceSuite struct {
	common.SnappySuite
	snapPath string
}

func (s *networkInterfaceSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	var err error
	s.snapPath, err = build.LocalSnap(c, data.NetworkConsumerSnapName)
	c.Assert(err, check.IsNil)

	common.InstallSnap(c, s.snapPath)
}

func (s *networkInterfaceSuite) TearDownTest(c *check.C) {
	s.SnappySuite.TearDownTest(c)

	os.Remove(s.snapPath)
	common.RemoveSnap(c, data.NetworkConsumerSnapName)
}

func (s *networkInterfaceSuite) TestPlugIsAutoconnected(c *check.C) {
	output, err := cli.ExecCommandErr("snap", "interfaces")
	c.Assert(err, check.IsNil)

	c.Assert(output, check.Matches, connectedPattern)
}

func (s *networkInterfaceSuite) TestPlugCanBeReconnected(c *check.C) {
	_, err := cli.ExecCommandErr("sudo", "snap", "disconnect",
		"network-consumer:network", "ubuntu-core:network")
	c.Assert(err, check.IsNil)

	output, err := cli.ExecCommandErr("snap", "interfaces")
	c.Assert(err, check.IsNil)
	c.Assert(output, check.Matches, disconnectedPattern)

	output, err = cli.ExecCommandErr("sudo", "snap", "connect",
		"network-consumer:network", "ubuntu-core:network")
	c.Assert(err, check.IsNil)

	output, err = cli.ExecCommandErr("snap", "interfaces")
	c.Assert(err, check.IsNil)
	c.Assert(output, check.Matches, connectedPattern)
}

func (s *networkInterfaceSuite) TestPlugDisconnectionDisablesFunctionality(c *check.C) {
	output, err := cli.ExecCommandErr("snap", "interfaces")
	c.Assert(err, check.IsNil)
	c.Assert(output, check.Matches, connectedPattern)

	output, err = cli.ExecCommandErr("network-consumer")
	c.Assert(err, check.IsNil)
	c.Assert(output, check.Equals, networkAccessibleOutput)

	_, err = cli.ExecCommandErr("sudo", "snap", "disconnect",
		"network-consumer:network", "ubuntu-core:network")
	c.Assert(err, check.IsNil)

	output, err = cli.ExecCommandErr("snap", "interfaces")
	c.Assert(err, check.IsNil)
	c.Assert(output, check.Matches, disconnectedPattern)

	output, err = cli.ExecCommandErr("network-consumer")
	c.Assert(err, check.IsNil)
	c.Assert(output == networkAccessibleOutput, check.Equals, false)
}
