/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package main

import (
	"fmt"
	"os"
	"os/exec"

	"launchpad.net/snappy/snappy"
)

const clickReview = "click-review"

type cmdBuild struct {
}

func init() {
	var cmdBuildData cmdBuild
	cmd, _ := parser.AddCommand("build",
		"Build a package",
		"Creates a snapp package",
		&cmdBuildData)

	cmd.Aliases = append(cmd.Aliases, "bu")
}

func (x *cmdBuild) Execute(args []string) (err error) {
	if len(args) == 0 {
		args = []string{"."}
	}

	snapPackage, err := snappy.Build(args[0])
	if err != nil {
		return err
	}

	cmd := exec.Command(clickReview, snapPackage)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// we ignore the error for now
	_ = cmd.Run()

	fmt.Printf("Generated '%s' snap\n", snapPackage)
	return nil
}
