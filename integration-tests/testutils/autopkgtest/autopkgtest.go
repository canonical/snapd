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

package autopkgtest

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/integration-tests/testutils/testutils"
	"github.com/snapcore/snapd/integration-tests/testutils/tpl"
)

const (
	controlTpl    = "integration-tests/data/tpl/control"
	dataOutputDir = "integration-tests/data/output/"
)

var (
	controlFile = filepath.Join(dataOutputDir, "control")
	// dependency aliasing
	execCommand      = testutils.ExecCommand
	prepareTargetDir = testutils.PrepareTargetDir
	tplExecute       = tpl.Execute
)

// AutoPkgTest is the type that knows how to call adt-run
type AutoPkgTest struct {
	// SourceCodePath is the location of the source code on the host.
	SourceCodePath string
	// TestArtifactsPath is the location of the test artifacts on the host.
	TestArtifactsPath string
	// TestFilter is an optional string to select a subset of tests.
	TestFilter string
	// IntegrationTestName is the name of the binary that runs the integration tests.
	IntegrationTestName string
	// ShellOnFail is used in case of failure to open a shell on the testbed before shutting it down.
	ShellOnFail bool
	// Env is a map with the environment variables to set on the test bed and their values.
	Env map[string]string
	// Verbose controls the amount of output printed
	Verbose bool
}

// AdtRunLocal starts a kvm running the image passed as argument and runs the
// autopkgtests using it as the testbed.
func (a *AutoPkgTest) AdtRunLocal(imgPath string) error {
	// Run the tests on the latest rolling edge image.
	return a.adtRun(kvmSSHOptions(imgPath))
}

// AdtRunRemote runs the autopkgtests using a remote machine as the testbed.
func (a *AutoPkgTest) AdtRunRemote(testbedIP string, testbedPort int) error {
	return a.adtRun(remoteTestbedSSHOptions(testbedIP, testbedPort))
}

func (a *AutoPkgTest) adtRun(testbedOptions string) (err error) {
	if err = a.createControlFile(); err != nil {
		return
	}

	fmt.Println("Calling adt-run...")
	outputDir := filepath.Join(a.TestArtifactsPath, "output")
	prepareTargetDir(outputDir)

	cmd := []string{
		"adt-run", "-B"}
	if !a.Verbose {
		cmd = append(cmd, "-q")
	}
	cmd = append(cmd, []string{
		"--override-control", controlFile,
		"--built-tree", a.SourceCodePath,
		"--output-dir", outputDir,
		"--setup-commands", "touch /run/autopkgtest_no_reboot.stamp"}...)
	for envVar, value := range a.Env {
		cmd = append(cmd, "--env")
		cmd = append(cmd, fmt.Sprintf("%s=%s", envVar, value))
	}
	if a.ShellOnFail {
		cmd = append(cmd, "--shell-fail")
	}

	execCommand(append(cmd, strings.Fields(testbedOptions)...)...)

	return
}

func (a *AutoPkgTest) createControlFile() error {
	return tplExecute(controlTpl, controlFile,
		struct {
			Filter, Test string
		}{
			a.TestFilter, a.IntegrationTestName})
}
