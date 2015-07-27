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

package build

import (
	"fmt"
	"os"

	"launchpad.net/snappy/_integration-tests/helpers/utils"
)

const (
	// IntegrationTestName is the name of the test binary.
	IntegrationTestName = "integration.test"
	testsBinDir         = "_integration-tests/bin/"
)

func buildAssets(useSnappyFromBranch bool, arch string) {
	utils.PrepareTargetDir(testsBinDir)

	if useSnappyFromBranch {
		// FIXME We need to build an image that has the snappy from the branch
		// installed. --elopio - 2015-06-25.
		buildSnappyCLI(arch)
	}
	buildTests(arch)
}

func buildSnappyCLI(arch string) {
	fmt.Println("Building snappy CLI...")
	// On the root of the project we have a directory called snappy, so we
	// output the binary for the tests in the tests directory.
	utils.GoCall(arch, "build", "-o", testsBinDir+"snappy", "./cmd/snappy")
}

func buildTests(arch string) {
	fmt.Println("Building tests...")

	utils.GoCall(arch, "test", "-c", "./_integration-tests/tests")
	// XXX Go test 1.3 does not have the output flag, so we move the
	// binaries after they are generated.
	os.Rename("tests.test", testsBinDir+IntegrationTestName)
}
