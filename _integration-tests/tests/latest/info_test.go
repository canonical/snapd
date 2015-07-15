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

package latest

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	. "../common"

	check "gopkg.in/check.v1"
)

var _ = check.Suite(&infoSuite{})

type infoSuite struct {
	SnappySuite
}

func readConfig(c *check.C) map[string]string {
	b, err := ioutil.ReadFile("testconfig.json")
	c.Assert(
		err, check.IsNil, check.Commentf("Failed to read test config: %v", err))

	var decoded map[string]string
	err = json.Unmarshal(b, &decoded)
	c.Assert(
		err, check.IsNil, check.Commentf("Failed to decode test config: %v", err))
	return decoded
}

func (s *infoSuite) TestInfoMustPrintReleaseAndChannel(c *check.C) {
	infoOutput := ExecCommand(c, "snappy", "info")

	config := readConfig(c)
	expected := "(?ms)" +
		fmt.Sprintf("^release: ubuntu-core/%s/%s\n", config["release"], config["channel"]) +
		".*"

	c.Assert(infoOutput, check.Matches, expected)
}

func (s *infoSuite) TestInfoMustPrintInstalledApps(c *check.C) {
	InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		RemoveSnap(c, "hello-world")
	})
	infoOutput := ExecCommand(c, "snappy", "info")

	expected := "(?ms)" +
		".*" +
		"^apps: .*hello-world.*\n"
	c.Assert(infoOutput, check.Matches, expected)
}

func (s *infoSuite) TestInfoMustPrintInstalledFrameworks(c *check.C) {
	// TODO: replace with hello-dbus-fwk. -- elopio - 2015-07-14
	// https://bugs.launchpad.net/snappy/+bug/1474658
	InstallSnap(c, "docker")
	s.AddCleanup(func() {
		RemoveSnap(c, "docker")
	})
	infoOutput := ExecCommand(c, "snappy", "info")

	expected := "(?ms)" +
		".*" +
		"^frameworks: .*docker.*\n" +
		".*"
	c.Assert(infoOutput, check.Matches, expected)
}
