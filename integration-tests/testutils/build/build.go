// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/testutils"
)

const (
	buildTestCmdFmt = "go test%s -c ./integration-tests/tests"

	// IntegrationTestName is the name of the test binary.
	IntegrationTestName = "integration.test"
	defaultGoArm        = "7"
	testsBinDir         = "integration-tests/bin/"
)

var (
	// dependency aliasing
	execCommand      = testutils.ExecCommand
	prepareTargetDir = testutils.PrepareTargetDir
	osRename         = os.Rename
	osSetenv         = os.Setenv
	osGetenv         = os.Getenv

	// The output of the build commands for testing goes to the testsBinDir path,
	// which is under the integration-tests directory. The
	// integration-tests/test-wrapper script (Test-Command's entry point of
	// adt-run) takes care of including testsBinDir at the beginning of $PATH, so
	// that these binaries (if they exist) take precedence over the system ones
	buildSnappyCliCmd = "go build -tags=excludeintegration -o " +
		filepath.Join(testsBinDir, "snappy") + " ." + string(os.PathSeparator) + filepath.Join("cmd", "snappy")
	buildSnapdCmd = "go build -tags=excludeintegration -o " +
		filepath.Join(testsBinDir, "snapd") + " ." + string(os.PathSeparator) + filepath.Join("cmd", "snapd")
)

// Config comprises the parameters for the Assets function
type Config struct {
	UseSnappyFromBranch bool
	Arch, TestBuildTags string
}

// Assets builds the snappy and integration tests binaries for the target
// architecture.
func Assets(cfg *Config) {
	if cfg == nil {
		cfg = &Config{}
	}
	prepareTargetDir(testsBinDir)

	if cfg.UseSnappyFromBranch {
		// FIXME We need to build an image that has the snappy from the branch
		// installed. --elopio - 2015-06-25.
		buildSnappyCLI(cfg.Arch)
		buildSnapd(cfg.Arch)
	}
	buildTests(cfg.Arch, cfg.TestBuildTags)
}

func buildSnappyCLI(arch string) {
	fmt.Println("Building snappy CLI...")
	goCall(arch, buildSnappyCliCmd)
}

func buildSnapd(arch string) {
	fmt.Println("Building snapd...")
	goCall(arch, buildSnapdCmd)
}

func buildTests(arch, testBuildTags string) {
	fmt.Println("Building tests...")

	var tagText string
	if testBuildTags != "" {
		tagText = " -tags=" + testBuildTags
	}
	cmd := fmt.Sprintf(buildTestCmdFmt, tagText)

	goCall(arch, cmd)
	// XXX Go test 1.3 does not have the output flag, so we move the
	// binaries after they are generated.
	osRename("tests.test", testsBinDir+IntegrationTestName)
}

func goCall(arch string, cmd string) {
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
	execCommand(strings.Fields(cmd)...)
}
