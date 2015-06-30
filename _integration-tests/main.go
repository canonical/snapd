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
	"strings"
)

const (
	baseDir          = "/tmp/snappy-test"
	defaultRelease   = "rolling"
	defaultChannel   = "edge"
	defaultArch      = "amd64"
	latestRevision   = ""
	defaultSSHPort   = 22
	defaultGoArm     = "7"
	latestTestName   = "command1"
	failoverTestName = "command2"
	updateTestName   = "command3"
	shellTestName    = "command4"
)

var (
	imageDir         = filepath.Join(baseDir, "image")
	imageTarget      = filepath.Join(imageDir, "snappy.img")
	commonSSHOptions = []string{"---", "ssh"}
	kvmSSHOptions    = append(
		commonSSHOptions,
		[]string{
			"-s", "/usr/share/autopkgtest/ssh-setup/snappy",
			"--", "-i", imageTarget}...)
)

func setupAndRunTests(useSnappyFromBranch bool, arch, testbedIP string, testbedPort int) {
	os.Remove(snappyFromBranchCmd)
	os.Remove(snappyTestsCmd)

	if useSnappyFromBranch {
		// FIXME We need to build an image that has the snappy from the branch
		// installed. --elopio - 2015-06-25.
		buildSnappyCLI(arch)
	}
	buildTests(arch)

	rootPath := getRootPath()
	if testbedIP == "" {
		createImage(defaultRelease, defaultChannel, latestRevision)
		latestTests := []string{
			latestTestName, failoverTestName, shellTestName}
		for i := range latestTests {
			adtRun(rootPath, kvmSSHOptions, latestTests[i])
		}

		createImage(defaultRelease, defaultChannel, "-1")
		adtRun(rootPath, kvmSSHOptions, updateTestName)
	} else {
		execCommand("ssh-copy-id", "-p", strconv.Itoa(testbedPort),
			"ubuntu@"+testbedIP)
		adtRun(rootPath, remoteTestbedSSHOptions(testbedIP, testbedPort),
			shellTestName)
	}
}

func execCommand(cmds ...string) {
	fmt.Println(strings.Join(cmds, " "))
	cmd := exec.Command(cmds[0], cmds[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("Error while running %s: %s\n", cmd.Args, err)
	}
}

func buildSnappyCLI(arch string) {
	fmt.Println("Building snappy CLI...")
	goCall(arch, "build", "-o", snappyFromBranchCmd, "./cmd/snappy")
}

func buildTests(arch string) {
	fmt.Println("Building tests...")
	tests := []string{"latest", "failover", "update"}
	for i := range tests {
		testName := tests[i]
		goCall("go", "test", "-c",
			"./_integration-tests/tests/"+testName)
	}
}

func goCall(arch string, cmds ...string) {
	if arch != "" {
		defer os.Setenv("GOARCH", os.Getenv("GOARCH"))
		os.Setenv("GOARCH", arch)
		if arch == "arm" {
			defer os.Setenv("GOARM", os.Getenv("GOARM"))
			os.Setenv("GOARM", defaultGoArm)
		}
	}
	goCmd := append([]string{"go"}, cmds...)
	execCommand(goCmd...)
}

func createImage(release, channel, revision string) {
	fmt.Println("Creating image...")
	prepareTargetDir(imageDir)
	udfCommand := []string{"sudo", "ubuntu-device-flash", "--verbose"}
	if revision != latestRevision {
		udfCommand = append(udfCommand, "--revision", revision)
	}
	coreOptions := []string{
		"core", release,
		"--output", imageTarget,
		"--channel", channel,
		"--developer-mode",
	}
	execCommand(append(udfCommand, coreOptions...)...)
}

func adtRun(rootPath string, testbedOptions []string, testname string) {
	fmt.Println("Calling adt-run...")
	outputDir := filepath.Join(baseDir, "output")
	prepareTargetDir(outputDir)

	cmd := []string{
		"adt-run", "-B",
		"--override-control", "debian/integration-tests/control"}

	cmd = append(cmd, "--testname", testname)

	cmd = append(cmd, []string{
		"--setup-commands", "touch /run/autopkgtest_no_reboot.stamp",
		"--override-control", "debian/integration-tests/control",
		"--built-tree", rootPath,
		"--output-dir", outputDir}...)

	execCommand(append(cmd, testbedOptions...)...)
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
		useSnappyFromBranch = flag.Bool("snappy-from-branch", false,
			"If this flag is used, snappy will be compiled from this branch, copied to the testbed and used for the tests. Otherwise, the snappy installed with the image will be used.")
		arch = flag.String("arch", "",
			"Architecture of the test bed. Defaults to use the same architecture as the host.")
		testbedIP = flag.String("ip", "",
			"IP of the testbed. If no IP is passed, a virtual machine will be created for the test.")
		testbedPort = flag.Int("port", defaultSSHPort,
			"SSH port of the testbed. Defaults to use port 22.")
	)

	flag.Parse()

	setupAndRunTests(*useSnappyFromBranch, *arch, *testbedIP, *testbedPort)
}
