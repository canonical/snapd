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
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&networkInterfaceSuite{
	interfaceSuite: interfaceSuite{
		sampleSnap:  data.NetworkConsumerSnapName,
		slot:        "network",
		plug:        "network-consumer",
		autoconnect: true}})

type networkInterfaceSuite struct {
	interfaceSuite
}

func (s *networkInterfaceSuite) TestPlugDisconnectionDisablesFunctionality(c *check.C) {
	output := cli.ExecCommand(c, "network-consumer")
	c.Assert(output, check.Equals, okOutput)

	cli.ExecCommand(c, "sudo", "snap", "disconnect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	output = cli.ExecCommand(c, "snap", "interfaces")
	c.Assert(output, check.Matches, disconnectedPattern(s.slot, s.plug))

	output = cli.ExecCommand(c, "network-consumer")
	c.Assert(output == okOutput, check.Equals, false)
}
