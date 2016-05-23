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
	"fmt"
	"os"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

const (
	connectedPatternFmt = "(?ms)" +
		"Slot +Plug\n" +
		".*" +
		"^:%s +%s\n" +
		".*"
	disconnectedPatternFmt = "(?ms)" +
		"Slot +Plug\n" +
		".*" +
		"^:%s +-\n" +
		".*" +
		"^- +%s:%s\n" +
		".*"
	okOutput = "ok\n"
)

type interfaceSuite struct {
	common.SnappySuite
	snapPaths, sampleSnaps []string
	slot, plug             string
	autoconnect            bool
}

func (s *interfaceSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	var err error

	s.snapPaths = make([]string, len(s.sampleSnaps))
	for i := 0; i < len(s.sampleSnaps); i++ {
		s.snapPaths[i], err = build.LocalSnap(c, s.sampleSnaps[i])
		c.Assert(err, check.IsNil)

		common.InstallSnap(c, s.snapPaths[i])
	}
}

func (s *interfaceSuite) TearDownTest(c *check.C) {
	s.SnappySuite.TearDownTest(c)

	for i := 0; i < len(s.snapPaths); i++ {
		os.Remove(s.snapPaths[i])
		common.RemoveSnap(c, s.sampleSnaps[i])
	}
}

func (s *interfaceSuite) TestPlugAutoconnect(c *check.C) {
	output, err := cli.ExecCommandErr("snap", "interfaces")
	c.Assert(err, check.IsNil)

	var pattern string
	if s.autoconnect {
		pattern = connectedPattern(s.slot, s.plug)
	} else {
		pattern = disconnectedPattern(s.slot, s.plug)
	}
	c.Assert(output, check.Matches, pattern)
}

func (s *interfaceSuite) TestPlugCanBeReconnected(c *check.C) {
	if !s.autoconnect {
		cli.ExecCommand(c, "sudo", "snap", "connect",
			s.plug+":"+s.slot, "ubuntu-core:"+s.slot)
	}

	cli.ExecCommand(c, "sudo", "snap", "disconnect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	output := cli.ExecCommand(c, "snap", "interfaces")
	c.Assert(output, check.Matches, disconnectedPattern(s.slot, s.plug))

	cli.ExecCommand(c, "sudo", "snap", "connect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	output = cli.ExecCommand(c, "snap", "interfaces")
	c.Assert(output, check.Matches, connectedPattern(s.slot, s.plug))
}

func connectedPattern(slot, plug string) string {
	return fmt.Sprintf(connectedPatternFmt, slot, plug)
}

func disconnectedPattern(slot, plug string) string {
	return fmt.Sprintf(disconnectedPatternFmt, slot, plug, slot)
}
