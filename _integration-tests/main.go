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
	"text/template"
)

const (
	baseDir        = "/tmp/snappy-test"
	testsBinDir    = "_integration-tests/bin/"
	defaultRelease = "rolling"
	defaultChannel = "edge"
	latestRevision = ""
	defaultSSHPort = 22
	defaultGoArm   = "7"
	controlFile    = "debian/integration-tests/control"
	controlTpl     = "_integration-tests/data/tpl/control"
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
	testPackagesLatest   = []string{"latest", "failover"}
	testPackagesPrevious = []string{"update"}
	testPackages         = append(testPackagesLatest, testPackagesPrevious...)
)

func setupAndRunTests(useSnappyFromBranch bool, arch, testbedIP, testFilter string, testbedPort int) {
	prepareTargetDir(testsBinDir)

	if useSnappyFromBranch {
		// FIXME We need to build an image that has the snappy from the branch
		// installed. --elopio - 2015-06-25.
		buildSnappyCLI(arch)
	}
	buildTests(arch)

	rootPath := getRootPath()

	if testbedIP == "" {
		var includeShell bool
		if testFilter == "" {
			includeShell = true
		}
		createImage(defaultRelease, defaultChannel, latestRevision)
		adtRun(rootPath, testFilter, testPackagesLatest, kvmSSHOptions, includeShell)

		createImage(defaultRelease, defaultChannel, "-1")
		adtRun(rootPath, testFilter, testPackagesPrevious, kvmSSHOptions, false)
	} else {
		execCommand("ssh-copy-id", "-p", strconv.Itoa(testbedPort),
			"ubuntu@"+testbedIP)
		adtRun(rootPath, "", []string{}, remoteTestbedSSHOptions(testbedIP, testbedPort), true)
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
	// On the root of the project we have a directory called snappy, so we
	// output the binary for the tests in the tests directory.
	goCall(arch, "build", "-o", testsBinDir+"snappy", "./cmd/snappy")
}

func buildTests(arch string) {
	fmt.Println("Building tests...")

	for _, testName := range testPackages {
		goCall(arch, "test", "-c",
			"./_integration-tests/tests/"+testName)
		// XXX Go test 1.3 does not have the output flag, so we move the
		// binaries after they are generated.
		os.Rename(testName+".test", testsBinDir+testName+".test")
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

func adtRun(rootPath, testFilter string, testList, testbedOptions []string, includeShell bool) {
	createControlFile(testFilter, testList, includeShell)

	fmt.Println("Calling adt-run...")
	outputSubdir := getOutputSubdir(testList, includeShell)
	outputDir := filepath.Join(baseDir, "output", outputSubdir)
	prepareTargetDir(outputDir)

	cmd := []string{
		"adt-run", "-B",
		"--override-control", "debian/integration-tests/control",
		"--setup-commands", "touch /run/autopkgtest_no_reboot.stamp",
		"--override-control", controlFile,
		"--built-tree", rootPath,
		"--output-dir", outputDir}

	execCommand(append(cmd, testbedOptions...)...)
}

func createControlFile(testFilter string, testList []string, includeShellTest bool) {
	type controlData struct {
		Filter       string
		Tests        []string
		IncludeShell bool
	}

	tpl, err := template.ParseFiles(controlTpl)
	if err != nil {
		log.Fatalf("Error reading adt-run control template %s", controlTpl)
	}

	outputFile, err := os.Create(controlFile)
	if err != nil {
		log.Fatalf("Error creating control file %s", controlFile)
	}
	defer outputFile.Close()

	err = tpl.Execute(outputFile, controlData{Filter: testFilter, Tests: testList, IncludeShell: includeShellTest})
	if err != nil {
		log.Fatalf("execution: %s", err)
	}
}

func getOutputSubdir(testList []string, includeShell bool) string {
	output := strings.Join(testList, "-")
	if includeShell {
		output = output + "-shell"
	}
	return output
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
			"SSH port of the testbed. Defaults to use port "+strconv.Itoa(defaultSSHPort))
		testFilter = flag.String("filter", "",
			"Suites or tests to run, for instance MyTestSuite, MyTestSuite.FirstCustomTest or MyTestSuite.*CustomTest")
	)

	flag.Parse()

	setupAndRunTests(*useSnappyFromBranch, *arch, *testbedIP, *testFilter, *testbedPort)
}
