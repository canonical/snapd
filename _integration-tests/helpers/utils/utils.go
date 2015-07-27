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

package utils

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

const defaultGoArm = "7"

// PrepareTargetDir creates the given target directory, removing it previously if it didn't exist
func PrepareTargetDir(targetDir string) {
	if _, err := os.Stat(targetDir); err == nil {
		// dir exists, remove it
		os.RemoveAll(targetDir)
	}
	os.MkdirAll(targetDir, 0777)
}

// ExecCommand executes the given command and pipes the results to os.Stdout and os.Stderr, returning the resulting error
func ExecCommand(cmds ...string) error {
	fmt.Println(strings.Join(cmds, " "))

	cmd := exec.Command(cmds[0], cmds[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		log.Panicf("Error while running %s: %s\n", cmd.Args, err)
	}
	return err
}

// GoCall executes the given go command using the right GOARCH and GOARM
// environment variables.
func GoCall(arch string, cmds ...string) {
	if arch != "" {
		defer os.Setenv("GOARCH", os.Getenv("GOARCH"))
		os.Setenv("GOARCH", arch)
		if arch == "arm" {
			defer os.Setenv("GOARM", os.Getenv("GOARM"))
			os.Setenv("GOARM", defaultGoArm)
		}
	}
	goCmd := append([]string{"go"}, cmds...)
	ExecCommand(goCmd...)
}
