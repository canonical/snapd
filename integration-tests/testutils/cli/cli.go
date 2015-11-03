// -*- Mode: Go; indent-tabs-mode: t -*-
// +build integration

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

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/check.v1"
)

var execCommand = exec.Command

// ExecCommand executes a shell command and returns a string with the output
// of the command. In case of error, it will fail the test.
func ExecCommand(c *check.C, cmds ...string) string {
	output, err := ExecCommandErr(cmds...)
	c.Assert(err, check.IsNil, check.Commentf("Error: %v", output))
	return output
}

// ExecCommandToFile executes a shell command and saves the output of the
// command to a file. In case of error, it will fail the test.
func ExecCommandToFile(c *check.C, filename string, cmds ...string) {
	cmd := execCommand(cmds[0], cmds[1:]...)
	outfile, err := os.Create(filename)
	c.Assert(err, check.IsNil, check.Commentf("Error creating output file %s", filename))

	defer outfile.Close()
	cmd.Stdout = outfile

	err = cmd.Run()
	c.Assert(err, check.IsNil, check.Commentf("Error executing command '%v': %v", cmds, err))
}

// ExecCommandErr executes a shell command and returns a string with the output
// of the command and eventually the obtained error
func ExecCommandErr(cmds ...string) (output string, err error) {
	fmt.Println(strings.Join(cmds, " "))
	cmd := execCommand(cmds[0], cmds[1:]...)
	outputByte, err := cmd.CombinedOutput()
	output = string(outputByte)
	fmt.Print(output)
	return
}
