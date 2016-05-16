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

package tests

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/config"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/partition"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/report"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/runner"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/wait"
)

const (
	cfgDir           = "/etc/systemd/system/snapd.service.d"
	daemonBinaryPath = "/usr/lib/snapd/snapd"
)

func init() {
	c := &check.C{}
	// Workaround for bug https://bugs.launchpad.net/snappy/+bug/1498293
	// TODO remove once the bug is fixed
	// originally added by elopio - 2015-09-30 to the rollback test, moved
	// here by --fgimenez - 2015-10-15
	wait.ForFunction(c, "regular", partition.Mode)

	if _, err := os.Stat(config.DefaultFileName); err == nil {
		cli.ExecCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
		cli.ExecCommand(c, "sudo", "systemctl", "disable", "snappy-autopilot.timer")

		cfg, err := config.ReadConfig(config.DefaultFileName)
		c.Assert(err, check.IsNil, check.Commentf("Error reading config: %v", err))

		setUpSnapd(c, cfg.FromBranch, "")
	}
}

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	output := io.MultiWriter(
		os.Stdout,
		report.NewSubunitV2ParserReporter(&report.FileReporter{}))
	runner.TestingT(t, output)

	if _, err := os.Stat(config.DefaultFileName); err == nil {
		tearDownSnapd(&check.C{})
	}
}

func setUpSnapd(c *check.C, fromBranch bool, extraEnv string) {
	cli.ExecCommand(c, "sudo", "systemctl", "stop",
		"snapd.service", "snapd.socket")

	cli.ExecCommand(c, "sudo", "mkdir", "-p", cfgDir)

	if fromBranch {
		err := writeCoverageConfig()
		c.Assert(err, check.IsNil)
	}

	err := writeEnvConfig(extraEnv)
	c.Assert(err, check.IsNil)

	cli.ExecCommand(c, "sudo", "systemctl", "daemon-reload")

	cli.ExecCommand(c, "sudo", "systemctl", "start", "snapd.service")
}

func tearDownSnapd(c *check.C) {
	cli.ExecCommand(c, "sudo", "systemctl", "stop",
		"snapd.service")

	cli.ExecCommand(c, "sudo", "rm", "-rf", cfgDir)

	cli.ExecCommand(c, "sudo", "systemctl", "daemon-reload")

	cli.ExecCommand(c, "sudo", "systemctl", "start", "snapd.service")
}

// this function writes a config file for snapd.service which clears and overrides the default
// ExecStart setting adding the required flags for recording coverage info
func writeCoverageConfig() error {
	cfgFileName := "coverage.conf"
	cfgFile := filepath.Join(cfgDir, cfgFileName)

	binPath, err := filepath.Abs("integration-tests/bin/snapd")
	if err != nil {
		return err
	}
	cmd, err := cli.AddOptionsToCommand([]string{filepath.Base(binPath)})
	cmd[0] = binPath

	// the first ExecStart= is needed to reset the setting value according to
	// https://www.freedesktop.org/software/systemd/man/systemd.service.html
	cfgContent := []byte(fmt.Sprintf(`[Service]
ExecStart=
ExecStart=%s
`, strings.Join(cmd, " ")))

	fmt.Println("snapd coverage.conf:\n", string(cfgContent))

	tmpFile := "/tmp/snapd." + cfgFileName
	if err = ioutil.WriteFile(tmpFile, cfgContent, os.ModeExclusive); err != nil {
		return err
	}

	if _, err = cli.ExecCommandErr("sudo", "mv", tmpFile, cfgFile); err != nil {
		return err
	}
	return nil
}

func writeEnvConfig(extraEnv string) error {
	cfgFile := filepath.Join(cfgDir, "env.conf")
	// FIXME: for now pass a test-only trusted key through an env var
	trustedKey, err := filepath.Abs("integration-tests/data/trusted.acckey")
	if err != nil {
		return err
	}
	httpProxy := os.Getenv("http_proxy")
	httpsProxy := os.Getenv("https_proxy")
	noProxy := os.Getenv("no_proxy")

	cfgContent := []byte(fmt.Sprintf(`[Service]
Environment="SNAPPY_TRUSTED_ACCOUNT_KEY=%s" "%s"
Environment="http_proxy=%s"
Environment="https_proxy=%s"
Environment="no_proxy=%s"
`, trustedKey, extraEnv, httpProxy, httpsProxy, noProxy))

	if err = ioutil.WriteFile("/tmp/snapd.env.conf", cfgContent, os.ModeExclusive); err != nil {
		return err
	}

	if _, err = cli.ExecCommandErr("sudo", "mv", "/tmp/snapd.env.conf", cfgFile); err != nil {
		return err
	}
	return nil
}
