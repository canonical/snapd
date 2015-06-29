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
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"text/template"
)

const (
	baseDir        = "/tmp/snappy-test"
	defaultRelease = "rolling"
	defaultChannel = "edge"
	defaultArch    = "amd64"
	defaultSSHPort = 22
	defaultGoArm   = "7"
	controlFile    = "debian/integration-tests/control"
	adtrunTemplate = `Test-Command: ./snappy.tests -gocheck.vv -test.outputdir=$ADT_ARTIFACTS {{ if .Filter }}-gocheck.f {{ .Filter }}{{ end }}
Restrictions: allow-stderr
Depends: ubuntu-snappy-tests

Test-Command: ./_integration-tests/snappy-selftest --yes-really
Depends:
`
)

var (
	testsDir         = filepath.Join(baseDir, "tests")
	imageDir         = filepath.Join(baseDir, "image")
	outputDir        = filepath.Join(baseDir, "output")
	imageTarget      = filepath.Join(imageDir, "snappy.img")
	commonSSHOptions = []string{"---", "ssh"}
	kvmSSHOptions    = append(
		commonSSHOptions,
		[]string{
			"-s", "/usr/share/autopkgtest/ssh-setup/snappy",
			"--", "-i", imageTarget}...)
)

func setupAndRunTests(arch, testbedIP, testFilter string, testbedPort int) {
	buildTests(arch)

	rootPath := getRootPath()
	if testbedIP == "" {
		createImage(defaultRelease, defaultChannel)
		adtRun(rootPath, testFilter, kvmSSHOptions)
	} else {
		execCommand("ssh-copy-id", "-p", strconv.Itoa(testbedPort), "ubuntu@"+testbedIP)
		adtRun(rootPath, testFilter, remoteTestbedSSHOptions(testbedIP, testbedPort))
	}
}

func execCommand(cmds ...string) {
	cmd := exec.Command(cmds[0], cmds[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("Error while running %s: %s\n", cmd.Args, err)
	}
}

func buildTests(arch string) {
	fmt.Println("Building tests")
	prepareTargetDir(testsDir)
	if arch != "" {
		defer os.Setenv("GOARCH", os.Getenv("GOARCH"))
		os.Setenv("GOARCH", arch)
		if arch == "arm" {
			defer os.Setenv("GOARM", os.Getenv("GOARM"))
			os.Setenv("GOARM", defaultGoArm)
		}
	}
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

func adtRun(rootPath, testFilter string, testbedOptions []string) {
	fmt.Println("Calling adt-run...")
	prepareTargetDir(outputDir)

	createControlFile(testFilter)

	cmd := []string{
		"adt-run",
		"-B",
		"--setup-commands", "touch /run/autopkgtest_no_reboot.stamp",
		"--override-control", controlFile,
		"--built-tree", rootPath,
		"--output-dir", outputDir,
	}
	execCommand(append(cmd, testbedOptions...)...)
}

func createControlFile(testFilter string) {
	type controlData struct {
		Filter string
	}

	tpl, err := template.New("controlFile").Parse(adtrunTemplate)
	if err != nil {
		log.Fatal("Error creating template for cotrol file")
	}

	outputFile, err := os.Create(controlFile)
	if err != nil {
		log.Fatalf("Error creating control file %s", controlFile)
	}
	defer outputFile.Close()

	err = tpl.Execute(outputFile, controlData{Filter: testFilter})
	if err != nil {
		log.Fatalf("execution: %s", err)
	}
}

func remoteTestbedSSHOptions(testbedIP string, testbedPort int) []string {
	options := []string{
		"-H", testbedIP,
		"-p", strconv.Itoa(testbedPort),
		"-l", "ubuntu",
		"-i", filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa"),
		"--reboot"}
	return append(commonSSHOptions, options...)
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
	var (
		arch = flag.String("arch", "",
			"Architecture of the test bed. Defaults to use the same architecture as the host.")
		testbedIP = flag.String("ip", "",
			"IP of the testbed. If no IP is passed, a virtual machine will be created for the test.")
		testbedPort = flag.Int("port", defaultSSHPort,
			"SSH port of the testbed. Defaults to use port 22.")
		testFilter = flag.String("filter", "",
			"Suites or tests to run, for instance MyTestSuite, MyTestSuite.FirstCustomTest or MyTestSuite.*CustomTest")
	)

	flag.Parse()

	setupAndRunTests(*arch, *testbedIP, *testFilter, *testbedPort)
}
