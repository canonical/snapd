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

	"launchpad.net/snappy/_integration-tests/testutils"
)

const (
	controlTpl    = "_integration-tests/data/tpl/control"
	dataOutputDir = "_integration-tests/data/output/"
)

var controlFile = filepath.Join(dataOutputDir, "control")

// Autopkgtest is the type that knows how to call adt-run
type Autopkgtest struct {
	sourceCodePath      string // location of the source code on the host
	testArtifactsPath   string // location of the test artifacts on the host
	testFilter          string
	integrationTestName string
}

// NewAutopkgtest is the Autopkgtest constructor
func NewAutopkgtest(sourceCodePath, testArtifactsPath, testFilter, integrationTestName string) *Autopkgtest {
	return &Autopkgtest{
		sourceCodePath:      sourceCodePath,
		testArtifactsPath:   testArtifactsPath,
		testFilter:          testFilter,
		integrationTestName: integrationTestName}
}

// AdtRunLocal starts a kvm running the image passed as argument and runs the
// autopkgtests using it as the testbed.
func (a *Autopkgtest) AdtRunLocal(imgPath string) {
	// Run the tests on the latest rolling edge image.
	a.adtRun(kvmSSHOptions(imgPath))
}

// AdtRunRemote runs the autopkgtests using a remote machine as the testbed.
func (a *Autopkgtest) AdtRunRemote(testbedIP string, testbedPort int) {
	testutils.ExecCommand("ssh-copy-id", "-p", strconv.Itoa(testbedPort),
		"ubuntu@"+testbedIP)
	a.adtRun(remoteTestbedSSHOptions(testbedIP, testbedPort))
}

func (a *Autopkgtest) adtRun(testbedOptions []string) {
	a.createControlFile()

	fmt.Println("Calling adt-run...")
	outputDir := filepath.Join(a.testArtifactsPath, "output")
	testutils.PrepareTargetDir(outputDir)

	cmd := []string{
		"adt-run", "-B",
		"--setup-commands", "touch /run/autopkgtest_no_reboot.stamp",
		"--override-control", controlFile,
		"--built-tree", a.sourceCodePath,
		"--output-dir", outputDir}

	testutils.ExecCommand(append(cmd, testbedOptions...)...)
}

func (a *Autopkgtest) createControlFile() {
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
		controlData{Test: a.integrationTestName, Filter: a.testFilter})
	if err != nil {
		log.Panicf("execution: %s", err)
	}
}
