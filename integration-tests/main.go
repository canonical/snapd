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

package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/ubuntu-core/snappy/_integration-tests/testutils"
	"github.com/ubuntu-core/snappy/_integration-tests/testutils/autopkgtest"
	"github.com/ubuntu-core/snappy/_integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/_integration-tests/testutils/config"
	"github.com/ubuntu-core/snappy/_integration-tests/testutils/image"
)

const (
	defaultOutputDir = "/tmp/snappy-test"
	defaultRelease   = "rolling"
	defaultChannel   = "edge"
	defaultSSHPort   = 22
	dataOutputDir    = "_integration-tests/data/output/"
)

var configFileName = filepath.Join(dataOutputDir, "testconfig.json")

func main() {
	var (
		useSnappyFromBranch = flag.Bool("snappy-from-branch", false,
			"If this flag is used, snappy will be compiled from this branch, copied to the testbed and used for the tests. Otherwise, the snappy installed with the image will be used.")
		arch = flag.String("arch", "",
			"Architecture of the test bed. Defaults to use the same architecture as the host.")
		testbedIP = flag.String("ip", "",
			"IP of the testbed. If no IP is passed, a virtual machine will be created for the test.")
		testbedPort = flag.Int("port", defaultSSHPort,
			"SSH port of the testbed. Defaults to use port "+strconv.Itoa(defaultSSHPort))
		testFilter = flag.String("filter", "",
			"Suites or tests to run, for instance MyTestSuite, MyTestSuite.FirstCustomTest or MyTestSuite.*CustomTest")
		imgRelease = flag.String("release", defaultRelease,
			"Release of the image to be built, defaults to "+defaultRelease)
		imgChannel = flag.String("channel", defaultChannel,
			"Channel of the image to be built, defaults to "+defaultChannel)
		imgRevision = flag.String("revision", "",
			"Revision of the image to be built (can be relative to the latest available revision in the given release and channel as in -1), defaults to the empty string")
		update = flag.Bool("update", false,
			"If this flag is used, the image will be updated before running the tests.")
		targetRelease = flag.String("target-release", "",
			"If the update flag is used, the image will be updated to this release before running the tests.")
		targetChannel = flag.String("target-channel", "",
			"If the update flag is used, the image will be updated to this channel before running the tests.")
		rollback = flag.Bool("rollback", false,
			"If this flag is used, the image will be updated and then rolled back before running the tests.")
		outputDir = flag.String("output-dir", defaultOutputDir, "Directory where test artifacts will be stored.")
	)

	flag.Parse()

	build.Assets(*useSnappyFromBranch, *arch)

	// TODO: generate the files out of the source tree. --elopio - 2015-07-15
	testutils.PrepareTargetDir(dataOutputDir)
	defer os.RemoveAll(dataOutputDir)

	remoteTestbed := *testbedIP != ""

	// TODO: pass the config as arguments to the test binaries.
	// --elopio - 2015-07-15
	cfg := config.NewConfig(
		configFileName, *imgRelease, *imgChannel, *targetRelease, *targetChannel,
		remoteTestbed, *update, *rollback)
	cfg.Write()

	rootPath := testutils.RootPath()

	test := autopkgtest.NewAutopkgtest(rootPath, *outputDir, *testFilter, build.IntegrationTestName)
	if !remoteTestbed {
		img := image.NewImage(*imgRelease, *imgChannel, *imgRevision, *outputDir)

		if imagePath, err := img.UdfCreate(); err == nil {
			if err = test.AdtRunLocal(imagePath); err != nil {
				log.Panic(err.Error())
			}
		} else {
			log.Panic(err.Error())
		}
	} else {
		if err := test.AdtRunRemote(*testbedIP, *testbedPort); err != nil {
			log.Panic(err.Error())
		}
	}
}
