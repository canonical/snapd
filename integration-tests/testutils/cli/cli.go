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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/config"

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
	cmds, err := AddOptionsToCommand(cmds)
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
	cmds, err = AddOptionsToCommand(cmds)
	if err != nil {
		return
	}
	cmd := execCommand(cmds[0], cmds[1:]...)
	return ExecCommandWrapper(cmd)
}

// ExecCommandWrapper decorates the execution of the given command
func ExecCommandWrapper(cmd *exec.Cmd) (output string, err error) {
	fmt.Println(strings.Join(cmd.Args, " "))
	outputByte, err := cmd.CombinedOutput()
	output = removeCoverageInfo(string(outputByte))
	fmt.Print(output)
	return
}

// AddOptionsToCommand inserts the required coverage options in
// the given snappy command slice
func AddOptionsToCommand(cmds []string) ([]string, error) {
	index := findIndex(cmds, "snappy", "snap", "snapd")
	if index != -1 {
		cfg, err := config.ReadConfig(config.DefaultFileName)
		if err != nil {
			return []string{}, err
		}
		if cfg.FromBranch {
			return addCoverageOptions(cmds, index)
		}
	}
	return cmds, nil
}

// findIndex returns the index of the first of targetItems in items, -1 if any of them present
func findIndex(items []string, targetItems ...string) int {
	for index, elem := range items {
		for _, target := range targetItems {
			if elem == target {
				return index
			}
		}
	}
	return -1
}

func addCoverageOptions(cmds []string, index int) ([]string, error) {
	orig := make([]string, len(cmds))
	copy(orig, cmds)

	coveragePath := getCoveragePath()
	err := os.MkdirAll(coveragePath, os.ModePerm)
	if err != nil {
		return []string{}, err
	}

	tmpFile := getCoverFilename()
	coverprofile := filepath.Join(coveragePath, tmpFile)

	head := append(cmds[:index+1],
		[]string{"-test.run=^TestRunMain$", "-test.coverprofile=" + coverprofile}...)
	tail := orig[index+1:]

	return append(head, tail...), nil
}

func removeCoverageInfo(input string) string {
	output := input
	lines := strings.Split(input, "\n")
	l := len(lines)

	if l >= 3 && lines[l-3] == "PASS" &&
		strings.HasPrefix(lines[l-2], "coverage") &&
		lines[l-1] == "" {
		lines = lines[:l-3]
		output = strings.Join(lines, "\n") + "\n"
	}
	return output
}

func getCoverFilename() string {
	coverFile, _ := ioutil.TempFile(getCoveragePath(), "")
	coverFile.Close()
	return filepath.Base(coverFile.Name())
}

func getCoveragePath() string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), "coverage")
}
