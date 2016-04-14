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

// FIXME: We don't want `snap build` - however we use it currently
//        in the integration tests on a snap image. we can not install
//        snapcraft onto the snappy base image. So we will need to
//        get a minimal `snapcraft snap` package that does not pull
//        in the gazillion of snapcraft dependencies. Until this pkg
//        is available and on the image we need the tiny support for
//        `snap build` so that we have working integration tests.

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jessevdk/go-flags"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/snappy"
)

const clickReview = "click-review"

type cmdBuild struct {
	Output        string `long:"output" short:"o"`
	BuildSquashfs bool   `long:"squashfs"`
}

var longBuildHelp = i18n.G("Creates a snap package and if available, runs the review scripts.")

func init() {
	cmd := addCommand("build",
		i18n.G("Builds a snap package"),
		longBuildHelp,
		func() flags.Commander {
			return &cmdBuild{}
		})
	cmd.hidden = true
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
