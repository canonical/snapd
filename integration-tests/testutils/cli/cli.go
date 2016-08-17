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

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/snapcore/snapd/integration-tests/testutils/config"

	"gopkg.in/check.v1"
)

var execCommand = exec.Command

// ExecCommand executes a shell command and returns a string with the output
// of the command. In case of error, it will fail the test.
func ExecCommand(c *check.C, cmds ...string) string {
	output, err := ExecCommandErr(cmds...)
	c.Assert(err, check.IsNil, check.Commentf("Error for %v: %v", cmds, output))
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
	cmd := execCommand(cmds[0], cmds[1:]...)
	return ExecCommandWrapper(cmd)
}

// ExecCommandWrapper decorates the execution of the given command
func ExecCommandWrapper(cmd *exec.Cmd) (output string, err error) {
	cfg, err := config.ReadConfig(config.DefaultFileName)
	if err != nil {
		return "", err
	}
	if cfg.Verbose {
		fmt.Println(strings.Join(cmd.Args, " "))
	}
	outputByte, err := cmd.CombinedOutput()
	output = string(outputByte)
	if cfg.Verbose {
		fmt.Print(output)
	}
	return output, err
}
