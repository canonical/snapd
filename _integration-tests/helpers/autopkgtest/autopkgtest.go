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

package autopkgtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"text/template"

	"log"

	"launchpad.net/snappy/_integration-tests/helpers/build"
	"launchpad.net/snappy/_integration-tests/helpers/image"
	"launchpad.net/snappy/_integration-tests/helpers/utils"
)

const (
	controlTpl    = "_integration-tests/data/tpl/control"
	dataOutputDir = "_integration-tests/data/output/"
)

var controlFile = filepath.Join(dataOutputDir, "control")

// AdtRunLocal starts a kvm running the image passed as argument and runs the
// autopkgtests using it as the testbed.
func AdtRunLocal(rootPath, baseDir, testFilter string, img image.Image) {
	// Run the tests on the latest rolling edge image.
	if imagePath, err := img.UdfCreate(); err == nil {
		adtRun(rootPath, baseDir, testFilter, kvmSSHOptions(imagePath))
	}
}

// AdtRunRemote runs the autopkgtests using a remote machine as the testbed.
func AdtRunRemote(rootPath, baseDir, testFilter, testbedIP string, testbedPort int) {
	utils.ExecCommand("ssh-copy-id", "-p", strconv.Itoa(testbedPort),
		"ubuntu@"+testbedIP)
	adtRun(
		rootPath, baseDir, testFilter, remoteTestbedSSHOptions(testbedIP, testbedPort))
}

func adtRun(rootPath, baseDir, testFilter string, testbedOptions []string) {
	createControlFile(testFilter)

	fmt.Println("Calling adt-run...")
	outputDir := filepath.Join(baseDir, "output")
	utils.PrepareTargetDir(outputDir)

	cmd := []string{
		"adt-run", "-B",
		"--setup-commands", "touch /run/autopkgtest_no_reboot.stamp",
		"--override-control", controlFile,
		"--built-tree", rootPath,
		"--output-dir", outputDir}

	utils.ExecCommand(append(cmd, testbedOptions...)...)
}

func createControlFile(testFilter string) {
	type controlData struct {
		Filter string
		Test   string
	}

	tpl, err := template.ParseFiles(controlTpl)
	if err != nil {
		log.Panicf("Error reading adt-run control template %s", controlTpl)
	}

	outputFile, err := os.Create(controlFile)
	if err != nil {
		log.Panicf("Error creating control file %s", controlFile)
	}
	defer outputFile.Close()

	err = tpl.Execute(outputFile,
		controlData{Test: build.IntegrationTestName, Filter: testFilter})
	if err != nil {
		log.Panicf("execution: %s", err)
	}
}
