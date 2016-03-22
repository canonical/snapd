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
)

const clickReview = "click-review"

type cmdBuild struct {
	Output string `long:"output" short:"o"`
}

func init() {
	cmd, err := parser.AddCommand("build",
		i18n.G("deprecated in favour of `snapcraft snap $DIR`"),
		i18n.G("deprecated in favour of `snapcraft snap $DIR`"),
		&cmdBuild{})
	if err != nil {
		logger.Panicf("Unable to build: %v", err)
	}

	cmd.Aliases = append(cmd.Aliases, "bu")
	cmd.Hidden = true
	addOptionDescription(cmd, "output", i18n.G("Specify an alternate output directory for the resulting package"))
}

func (x *cmdBuild) Execute(args []string) (err error) {
	if len(args) == 0 {
		args = []string{"."}
	}

	if _, err := exec.LookPath("snapcraft"); err != nil {
		fmt.Fprintf(os.Stderr, `please "sudo apt install snapcraft"`)
		os.Exit(1)
	}

	cmd := []string{"snapcraft", "snap"}
	cmd = append(cmd, args...)
	return exec.Command(cmd[0], cmd[1:]...).Run()
}
