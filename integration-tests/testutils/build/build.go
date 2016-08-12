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

package build

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/testutils"
)

const (
	buildTestCmdFmt = "go test%s -c ./integration-tests/tests"

	snapbuildPkg = "./tests/lib/snapbuild"

	// IntegrationTestName is the name of the test binary.
	IntegrationTestName = "integration.test"
	defaultGoArm        = "7"
	testsBinDir         = "integration-tests/bin/"
	projectSrcPath      = "src/github.com/snapcore/snapd"
)

var (
	// dependency aliasing
	execCommand      = cli.ExecCommandWrapper
	prepareTargetDir = testutils.PrepareTargetDir
	osRename         = os.Rename
	osSetenv         = os.Setenv
	osGetenv         = os.Getenv
)

// Config comprises the parameters for the Assets function
type Config struct {
	UseSnappyFromBranch bool
	Arch, TestBuildTags string
}

// Assets builds the snappy and integration tests binaries for the target
// architecture.
func Assets(cfg *Config) error {
	tmp := "/tmp/snappy-build"
	_, filename, _, _ := runtime.Caller(1)
	dir, _ := filepath.Abs(filepath.Join(path.Dir(filename), ".."))
	os.Symlink(dir, tmp)

	if cfg == nil {
		cfg = &Config{}
	}
	prepareTargetDir(testsBinDir)

	if cfg.UseSnappyFromBranch {
		// FIXME We need to build an image that has the snappy from the branch
		// installed. --elopio - 2015-06-25.
		if err := buildSnapd(cfg.Arch); err != nil {
			return err
		}
		if err := buildSnapCLI(cfg.Arch); err != nil {
			return err
		}
	}
	if err := buildSnapbuild(cfg.Arch); err != nil {
		return err
	}
	return buildTests(cfg.Arch, cfg.TestBuildTags)
}

func buildSnapd(arch string) error {
	fmt.Println("Building snapd...")
	buildSnapdCmd := getBinaryBuildCmd("snapd")

	return goCall(arch, buildSnapdCmd)
}

func buildSnapCLI(arch string) error {
	fmt.Println("Building snap...")

	buildSnapCliCmd := getBinaryBuildCmd("snap")
	return goCall(arch, buildSnapCliCmd)
}

func buildSnapbuild(arch string) error {
	fmt.Println("Building snapbuild...")

	buildSnapbuildCmd := "go build" +
		" -o " + filepath.Join(testsBinDir, filepath.Base(snapbuildPkg)) + " " + snapbuildPkg
	return goCall(arch, buildSnapbuildCmd)
}

func buildTests(arch, testBuildTags string) error {
	fmt.Println("Building tests...")

	var tagText string
	if testBuildTags != "" {
		tagText = " -tags=" + testBuildTags
	}
	cmd := fmt.Sprintf(buildTestCmdFmt, tagText)

	if err := goCall(arch, cmd); err != nil {
		return err
	}
	// XXX Go test 1.3 does not have the output flag, so we move the
	// binaries after they are generated.
	return osRename("tests.test", testsBinDir+IntegrationTestName)
}

func goCall(arch string, cmd string) error {
	if arch != "" {
		defer osSetenv("GOARCH", osGetenv("GOARCH"))
		osSetenv("GOARCH", arch)
		if arch == "arm" {
			envs := map[string]string{
				"GOARM":       defaultGoArm,
				"CGO_ENABLED": "1",
				"CC":          "arm-linux-gnueabihf-gcc",
			}
			for env, value := range envs {
				defer osSetenv(env, osGetenv(env))
				osSetenv(env, value)
			}
		}
	}
	cmdElems := strings.Fields(cmd)
	command := exec.Command(cmdElems[0], cmdElems[1:]...)
	command.Dir = filepath.Join(os.Getenv("GOPATH"), projectSrcPath)
	output, err := execCommand(command)
	if err != nil {
		return fmt.Errorf("command %q failed: %q (%s)", cmdElems, err, output)
	}
	return nil
}

func getBinaryBuildCmd(binary string) string {
	// The output of the build commands for testing goes to the testsBinDir path,
	// which is under the integration-tests directory. The
	// integration-tests/test-wrapper script (Test-Command's entry point of
	// adt-run) takes care of including testsBinDir at the beginning of $PATH, so
	// that these binaries (if they exist) take precedence over the system ones
	return "go build -o " + filepath.Join(testsBinDir, binary) + " ." +
		string(os.PathSeparator) + filepath.Join("cmd", binary)
}
