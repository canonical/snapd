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
		cfg, err := config.ReadConfig(config.DefaultFileName)
		if err != nil {
			t.Fatalf("Error reading config: %v", err)
		}

		if err := tearDownSnapd(cfg.FromBranch); err != nil {
			t.Fatalf("Error stopping daemon: %v", err)
		}
	}
}

func setUpSnapd(c *check.C, fromBranch bool, extraEnv string) {
	cli.ExecCommand(c, "sudo", "systemctl", "stop",
		"snapd.service", "snapd.socket")

	if fromBranch {
		binPath, err := filepath.Abs("integration-tests/bin/snapd")
		c.Assert(err, check.IsNil)

		_, err = cli.ExecCommandErr("sudo", "mount", "-o", "bind",
			binPath, daemonBinaryPath)
		c.Assert(err, check.IsNil)
	}

	err := writeEnvConfig(extraEnv)
	c.Assert(err, check.IsNil)

	_, err = cli.ExecCommandErr("sudo", "systemctl", "daemon-reload")
	c.Assert(err, check.IsNil)

	_, err = cli.ExecCommandErr("sudo", "systemctl", "start", "snapd.service")
	c.Assert(err, check.IsNil)
}

func tearDownSnapd(fromBranch bool) error {
	if _, err := cli.ExecCommandErr("sudo", "systemctl", "stop",
		"snapd.service"); err != nil {
		return err
	}

	if _, err := cli.ExecCommandErr("sudo", "rm", "-rf", cfgDir); err != nil {
		return err
	}

	if fromBranch {
		if _, err := cli.ExecCommandErr("sudo", "umount", daemonBinaryPath); err != nil {
			return err
		}
	}

	if _, err := cli.ExecCommandErr("sudo", "systemctl", "daemon-reload"); err != nil {
		return err
	}

	if _, err := cli.ExecCommandErr("sudo", "systemctl", "start",
		"snapd.service"); err != nil {
		return err
	}

	return nil
}

func writeEnvConfig(extraEnv string) error {
	if _, err := cli.ExecCommandErr("sudo", "mkdir", "-p", cfgDir); err != nil {
		return err
	}

	cfgFile := filepath.Join(cfgDir, "env.conf")
	// FIXME: for now pass a test-only trusted key through an env var
	trustedKey, err := filepath.Abs("integration-tests/data/trusted.acckey")
	if err != nil {
		return err
	}

	cfgContent := []byte(fmt.Sprintf(`[Service]
Environment="SNAPPY_TRUSTED_ACCOUNT_KEY=%s" "%s"
`, trustedKey, extraEnv))
	if err = ioutil.WriteFile("/tmp/snapd.env.conf", cfgContent, os.ModeExclusive); err != nil {
		return err
	}

	if _, err = cli.ExecCommandErr("sudo", "mv", "/tmp/snapd.env.conf", cfgFile); err != nil {
		return err
	}
	return nil
}
