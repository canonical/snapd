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
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	baseDir         = "/tmp/snappy-test"
	debsTestBedPath = "/tmp/snappy-debs"
	defaultRelease  = "rolling"
	defaultChannel  = "edge"
)

var (
	debsDir     = filepath.Join(baseDir, "debs")
	testsDir    = filepath.Join(baseDir, "tests")
	imageDir    = filepath.Join(baseDir, "image")
	outputDir   = filepath.Join(baseDir, "output")
	imageTarget = filepath.Join(imageDir, "snappy.img")
)

func execCommand(cmds ...string) {
	cmd := exec.Command(cmds[0], cmds[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("Error while running %s: %s\n", cmd.Args, err)
	}
}

func buildTests() {
	fmt.Println("Building tests")
	prepareTargetDir(testsDir)
	execCommand("go", "test", "-c", "./_integration-tests/tests")
	os.Rename("tests.test", "snappy.tests")
}

func createImage(release, channel string) {
	fmt.Println("Creating image...")
	prepareTargetDir(imageDir)
	execCommand(
		"sudo", "ubuntu-device-flash", "--verbose",
		"core", release,
		"-o", imageTarget,
		"--channel", channel,
		"--developer-mode")
}

func adtRun(rootPath string) {
	fmt.Println("Calling adt-run...")
	prepareTargetDir(outputDir)
	execCommand(
		"adt-run",
		"-B",
		"--setup-commands", "touch /run/autopkgtest_no_reboot.stamp",
		"--override-control", "debian/integration-tests/control",
		"--built-tree", rootPath,
		"--output-dir", outputDir,
		"---",
		"ssh", "-s", "/usr/share/autopkgtest/ssh-setup/snappy",
		"--", "-i", imageTarget)
}

func prepareTargetDir(targetDir string) {
	if _, err := os.Stat(targetDir); err == nil {
		// dir exists, remove it
		os.RemoveAll(targetDir)
	}
	os.MkdirAll(targetDir, 0777)
}

func getRootPath() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return dir
}

func main() {
	rootPath := getRootPath()

	buildTests()

	createImage(defaultRelease, defaultChannel)

	adtRun(rootPath)
}
