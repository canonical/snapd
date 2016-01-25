// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package testutils

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// PrepareTargetDir creates the given target directory, removing it previously if it didn't exist
func PrepareTargetDir(targetDir string) {
	if _, err := os.Stat(targetDir); err == nil {
		// dir exists, remove it
		os.RemoveAll(targetDir)
	}
	os.MkdirAll(targetDir, 0777)
}

// RootPath return the test current working directory.
func RootPath() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Panic(err)
	}
	return dir
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

// RemoveTestFlags strips the flags beginning with "-test." from os.Args,
// useful for the test binaries compiled as the original cmds for measuring
// integration test coverage
func RemoveTestFlags() {
	outputArgs := []string{}
	for _, item := range os.Args {
		if !strings.HasPrefix(item, "-test.") {
			outputArgs = append(outputArgs, item)
		}
	}
	os.Args = outputArgs
}
