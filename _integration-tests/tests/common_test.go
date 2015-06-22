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
	"os/exec"
	"testing"

	. "gopkg.in/check.v1"
)

type CommonSuite struct{}

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

func execCommand(c *C, cmds ...string) []byte {
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil, Commentf("Error: %v", string(output)))
	return output
}

func execCommandToFile(c *C, filename string, cmds ...string) {
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	outfile, err := os.Create(filename)
	c.Assert(err, IsNil, Commentf("Error creating output file %s", filename))

	defer outfile.Close()
	cmd.Stdout = outfile

	err = cmd.Run()
	c.Assert(err, IsNil, Commentf("Error executing command '%v': %v", cmds, err))
}

func (s *CommonSuite) SetUpSuite(c *C) {
	execCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
	execCommand(c, "sudo", "systemctl", "disable", "snappy-autopilot.timer")
}

func (s *CommonSuite) SetUpTest(c *C) {
	afterReboot := os.Getenv("ADT_REBOOT_MARK")

	if afterReboot == "" {
		c.Logf("****** Running %s", c.TestName())
	} else {
		if afterReboot == c.TestName() {
			c.Logf("****** Resuming %s after reboot", c.TestName())
		} else {
			c.Skip(fmt.Sprintf("****** Skipped %s after reboot caused by %s",
				c.TestName(), afterReboot))
		}
	}
}
