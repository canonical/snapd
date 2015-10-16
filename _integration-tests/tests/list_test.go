// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"fmt"
	"os"

	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/common"

	"github.com/mvo5/goconfigparser"
	"gopkg.in/check.v1"
)

var _ = check.Suite(&listSuite{})

type listSuite struct {
	common.SnappySuite
}

func getVersionFromConfig(c *check.C) string {
	cfg := goconfigparser.New()
	f, err := os.Open("/etc/system-image/channel.ini")
	c.Assert(err, check.IsNil,
		check.Commentf("Error opening the config file: %v:", err))
	defer f.Close()
	err = cfg.Read(f)
	c.Assert(err, check.IsNil,
		check.Commentf("Error parsing the config file: %v", err))
	version, err := cfg.Get("service", "build_number")
	c.Assert(err, check.IsNil,
		check.Commentf("Error getting the build number: %v", err))
	return version
}

func (s *listSuite) TestListMustPrintCoreVersion(c *check.C) {
	listOutput := cli.ExecCommand(c, "snappy", "list")

	expected := "(?ms)" +
		"Name +Date +Version +Developer *\n" +
		".*" +
		fmt.Sprintf("^ubuntu-core +.* +%s +ubuntu *\n", getVersionFromConfig(c)) +
		".*"
	c.Assert(listOutput, check.Matches, expected)
}

func (s *listSuite) TestListMustPrintAppVersion(c *check.C) {
	common.InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	listOutput := cli.ExecCommand(c, "snappy", "list")
	expected := "(?ms)" +
		"Name +Date +Version +Developer *\n" +
		".*" +
		"^hello-world +.* +(\\d+)(\\.\\d+)* +.* +.* *\n" +
		".*"

	c.Assert(listOutput, check.Matches, expected)
}
