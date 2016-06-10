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
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/data"
)

var _ = check.Suite(&logObserveInterfaceSuite{
	interfaceSuite: interfaceSuite{
		sampleSnaps: []string{data.LogObserveConsumerSnapName, data.NetworkConsumerSnapName},
		slot:        "log-observe",
		plug:        "log-observe-consumer"}})

type logObserveInterfaceSuite struct {
	interfaceSuite
}

func (s *logObserveInterfaceSuite) TestConnectedPlugAllowsLogObserve(c *check.C) {
	cli.ExecCommand(c, "sudo", "snap", "connect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	output := cli.ExecCommand(c, "network-consumer", "http://127.0.0.1:8081")
	c.Assert(output, check.Equals, "ok\n")
}

func (s *logObserveInterfaceSuite) TestDisconnectedPlugDisablesLogObserve(c *check.C) {
	output := cli.ExecCommand(c, "network-consumer", "http://127.0.0.1:8081")
	c.Assert(output, check.Equals, "error accessing log\n")
}
