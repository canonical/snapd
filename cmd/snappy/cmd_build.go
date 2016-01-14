// -*- Mode: Go; indent-tabs-mode: t -*-

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

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/snappy"
)

const clickReview = "click-review"

type cmdBuild struct {
	Output        string `long:"output" short:"o"`
	BuildSquashfs bool   `long:"squashfs"`
}

var longBuildHelp = i18n.G("Creates a snap package and if available, runs the review scripts.")

func init() {
	cmd, err := parser.AddCommand("build",
		i18n.G("Builds a snap package"),
		longBuildHelp,
		&cmdBuild{})
	if err != nil {
		logger.Panicf("Unable to build: %v", err)
	}

	cmd.Aliases = append(cmd.Aliases, "bu")
	addOptionDescription(cmd, "output", i18n.G("Specify an alternate output directory for the resulting package"))
}

func (x *cmdBuild) Execute(args []string) (err error) {
	if len(args) == 0 {
		args = []string{"."}
	}

	snapPackage, err := snappy.BuildSquashfsSnap(args[0], x.Output)
	if err != nil {
		return err
	}

	_, err = exec.LookPath(clickReview)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not review package (%s not available)\n", clickReview)
	} else {
		cmd := exec.Command(clickReview, snapPackage)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// we ignore the error for now
		_ = cmd.Run()
	}

	// TRANSLATORS: the %s is a pkgname
	fmt.Printf(i18n.G("Generated '%s' snap\n"), snapPackage)
	return nil
}
