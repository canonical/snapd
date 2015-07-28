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
	"text/template"

	"log"

	"launchpad.net/snappy/_integration-tests/testutils"
	"launchpad.net/snappy/_integration-tests/testutils/build"
)

const (
	controlTpl    = "_integration-tests/data/tpl/control"
	dataOutputDir = "_integration-tests/data/output/"
)

var controlFile = filepath.Join(dataOutputDir, "control")

// AdtRun runs the autopkgtests.
func AdtRun(rootPath, baseDir, testFilter string, testbedOptions []string) {
	createControlFile(testFilter)

	fmt.Println("Calling adt-run...")
	outputDir := filepath.Join(baseDir, "output")
	testutils.PrepareTargetDir(outputDir)

	cmd := []string{
		"adt-run", "-B",
		"--setup-commands", "touch /run/autopkgtest_no_reboot.stamp",
		"--override-control", controlFile,
		"--built-tree", rootPath,
		"--output-dir", outputDir}

	testutils.ExecCommand(append(cmd, testbedOptions...)...)
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
