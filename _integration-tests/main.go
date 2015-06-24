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
)

const (
	baseDir         = "/tmp/snappy-test"
	debsTestBedPath = "/tmp/snappy-debs"
	defaultRelease  = "rolling"
	defaultChannel  = "edge"
	defaultArch     = "amd64"
)

var (
	defaultDebsDir   = filepath.Join(baseDir, "debs")
	imageDir         = filepath.Join(baseDir, "image")
	outputDir        = filepath.Join(baseDir, "output")
	imageTarget      = filepath.Join(imageDir, "snappy.img")
	commonSSHOptions = []string{"---", "ssh"}
	kvmSSHOptions    = append(
		commonSSHOptions,
		[]string{
			"-s", "/usr/share/autopkgtest/ssh-setup/snappy",
			"--", "-i", imageTarget}...)
	useFlashedImage bool
	arch            string
	testbedIP       string
	debsDir         string
)

func init() {
	flag.BoolVar(&useFlashedImage, "installed-image", false, "Wether we should install the snappy version from the branch or use the one installed on the image")
	flag.StringVar(&debsDir, "debs-dir", defaultDebsDir, "Directory with the snappy debian packages.")
	flag.StringVar(&arch, "arch", defaultArch, "Target architecture (amd64, armhf)")
	flag.StringVar(&testbedIP, "ip", "", "IP of the testbed to run the tests in")
}

func execCommand(cmds ...string) {
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("Error while running %s: %s\n", cmd.Args, err)
	}
}

func buildDebs(rootPath, arch string) {
	buildCommand := []string{"bzr", "bd",
		fmt.Sprintf("--result-dir=%s", debsDir),
		"--split",
		rootPath,
	}
	if arch != defaultArch {
		builderOption := []string{
			fmt.Sprintf(
				"--builder='sbuild --build amd64 --host %s --dist wily'", arch)}
		buildCommand = append(buildCommand, builderOption...)
	} else {
		dontSignDebs := []string{"--", "-uc", "-us"}
		buildCommand = append(buildCommand, dontSignDebs...)
	}
	fmt.Println(buildCommand)
	execCommand(buildCommand...)
}

func createImage(release, channel, arch string) {
	fmt.Println("Creating image...")
	prepareTargetDir(imageDir)
	execCommand(
		"sudo", "ubuntu-device-flash", "--verbose",
		"core", release,
		"-o", imageTarget,
		fmt.Sprintf("--oem=%s", arch),
		"--channel", channel,
		"--developer-mode")
}

func adtRun(rootPath string, testbedOptions []string) {
	fmt.Println("Calling adt-run...")
	prepareTargetDir(outputDir)
	cmd := []string{
		"adt-run", "-B",
		"--override-control", "debian/integration-tests/control",
		"--built-tree", rootPath,
		"--output-dir", outputDir}

	if !useFlashedImage {
		debsSetup := []string{
			"--setup-commands", "touch /run/autopkgtest_no_reboot.stamp",
			"--setup-commands", "mount -o remount,rw /",
			"--setup-commands",
			fmt.Sprintf("dpkg -i %s/*deb", debsTestBedPath),
			"--setup-commands",
			"sync; sleep 2; mount -o remount,ro /",
			fmt.Sprintf("--copy=%s:%s", debsDir, debsTestBedPath)}
		cmd = append(cmd, debsSetup...)
	}

	execCommand(append(cmd, testbedOptions...)...)
}

func remoteTestbedSSHOptions(testbedIP string) []string {
	options := []string{
		"-H", testbedIP,
		"-l", "ubuntu",
		"-i", filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa")}
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

func getArchForImage() string {
	return fmt.Sprintf("generic-%s", defaultArch)
}

func main() {
	flag.Parse()

	rootPath := getRootPath()

	if !useFlashedImage && debsDir == defaultDebsDir {
		buildDebs(rootPath, arch)
	}
	if testbedIP == "" {
		createImage(defaultRelease, defaultChannel, getArchForImage())
		adtRun(rootPath, kvmSSHOptions)
	} else {
		execCommand("ssh-copy-id", "ubuntu@"+testbedIP)
		adtRun(rootPath, remoteTestbedSSHOptions(testbedIP))
	}
}
