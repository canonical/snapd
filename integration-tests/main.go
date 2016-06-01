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

package main

import (
	"flag"
	"log"
	"os"
	"strconv"

	"github.com/snapcore/snapd/integration-tests/testutils/autopkgtest"
	"github.com/snapcore/snapd/integration-tests/testutils/build"
	"github.com/snapcore/snapd/integration-tests/testutils/config"
	"github.com/snapcore/snapd/integration-tests/testutils/image"
	"github.com/snapcore/snapd/integration-tests/testutils/testutils"
)

const (
	defaultOutputDir = "/tmp/snappy-test"
	defaultRelease   = "16"
	defaultChannel   = "edge"
	defaultSSHPort   = 22
	dataOutputDir    = "integration-tests/data/output/"

	defaultKernel = "canonical-pc-linux"
	defaultOS     = "ubuntu-core"
	defaultGadget = "canonical-pc"
)

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

		imgOS = flag.String("os", defaultOS,
			"OS snap of the image to be built, defaults to "+defaultOS)
		imgKernel = flag.String("kernel", defaultKernel,
			"Kernel snap of the image to be built, defaults to "+defaultKernel)
		imgGadget = flag.String("gadget", defaultGadget,
			"Gadget snap of the image to be built, defaults to "+defaultGadget)

		update = flag.Bool("update", false,
			"If this flag is used, the image will be updated before running the tests.")
		rollback = flag.Bool("rollback", false,
			"If this flag is used, the image will be updated and then rolled back before running the tests.")
		outputDir     = flag.String("output-dir", defaultOutputDir, "Directory where test artifacts will be stored.")
		shellOnFail   = flag.Bool("shell-fail", false, "Run a shell in the testbed if the suite fails.")
		testBuildTags = flag.String("test-build-tags", "allsnaps", "Build tags to be passed to the go test command")
		httpProxy     = flag.String("http-proxy", "", "HTTP proxy to set in the testbed.")
		verbose       = flag.Bool("v", false, "Show complete test output")
	)

	flag.Parse()

	// TODO: generate the files out of the source tree. --elopio - 2015-07-15
	testutils.PrepareTargetDir(dataOutputDir)
	defer os.RemoveAll(dataOutputDir)

	remoteTestbed := *testbedIP != ""

	// TODO: pass the config as arguments to the test binaries.
	// --elopio - 2015-07-15
	cfg := &config.Config{
		FileName:      config.DefaultFileName,
		Release:       *imgRelease,
		Channel:       *imgChannel,
		RemoteTestbed: remoteTestbed,
		Update:        *update,
		Rollback:      *rollback,
		FromBranch:    *useSnappyFromBranch,
		Verbose:       *verbose,
	}
	cfg.Write()

	build.Assets(&build.Config{
		UseSnappyFromBranch: *useSnappyFromBranch,
		Arch:                *arch,
		TestBuildTags:       *testBuildTags})

	rootPath := testutils.RootPath()

	test := &autopkgtest.AutoPkgTest{
		SourceCodePath:      rootPath,
		TestArtifactsPath:   *outputDir,
		TestFilter:          *testFilter,
		IntegrationTestName: build.IntegrationTestName,
		ShellOnFail:         *shellOnFail,
		Env: map[string]string{
			"http_proxy":         *httpProxy,
			"https_proxy":        *httpProxy,
			"no_proxy":           "127.0.0.1,127.0.1.1,localhost,login.ubuntu.com",
			"TEST_USER_NAME":     os.Getenv("TEST_USER_NAME"),
			"TEST_USER_PASSWORD": os.Getenv("TEST_USER_PASSWORD"),
		},
		Verbose: *verbose,
	}
	if !remoteTestbed {
		img := &image.Image{
			Release:  *imgRelease,
			Channel:  *imgChannel,
			Revision: *imgRevision,
			OS:       *imgOS,
			Kernel:   *imgKernel,
			Gadget:   *imgGadget,
			BaseDir:  *outputDir}

		if imagePath, err := img.UdfCreate(); err == nil {
			if err = test.AdtRunLocal(imagePath); err != nil {
				log.Panicf("%s", err)
			}
		} else {
			log.Panicf("%s", err)
		}
	} else {
		if err := test.AdtRunRemote(*testbedIP, *testbedPort); err != nil {
			log.Panicf("%s", err)
		}
	}
}
